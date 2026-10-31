import { Router } from 'express';
import { ConflictError, InvalidTreeError } from '../store.js';

/**
 * Creates the unified fzt-frontend tree routes.
 *
 * Every tree is an entity with a flat string id (e.g. `<sub>-menu`,
 * `<sub>-bookmarks`, `google`). No namespace, no ACL scoping — any
 * authenticated caller can read/write any tree. Identity-based scoping
 * is enforced client-side by choosing which ids to fetch/save.
 *
 * Trees live in the romaine-life/at repo as trees/<id>.json
 * ({ version, updatedAt, tree }); each save is a commit to its main that
 * bumps `version` by 1. A menu save must keep the store's invariants:
 * every entry runs a command the store has, and every command keeps an entry.
 *
 * Refs within a tree use the shape `{ ref: "<treeId>" }`. The GET
 * handler recursively expands them (cycle-guarded, depth-limited),
 * tagging resolved nodes with `_ref`/`_refVersion` so PUT can strip
 * them back to pointer form. Cross-tree writes are NOT performed — if
 * a caller wants to edit a referenced tree, they PUT that tree's id
 * directly (see fzt-frontend#4).
 *
 * @param {{
 *   requireAuth: Function,
 *   store: import('../store.js').Store,
 * }} opts
 */
export function createFztFrontendRoutes({ requireAuth, store }) {
  const router = Router();

  const MAX_REF_DEPTH = 10;

  async function resolveRefs(items, visited = new Set()) {
    if (!Array.isArray(items)) return items;
    const out = [];
    for (const item of items) {
      if (!item || typeof item !== 'object') {
        out.push(item);
        continue;
      }
      const keysWithoutError = Object.keys(item).filter(k => k !== '_refError');
      if (typeof item.ref === 'string' && keysWithoutError.length === 1) {
        if (visited.has(item.ref) || visited.size >= MAX_REF_DEPTH) {
          out.push({ ...item, _refError: true });
          continue;
        }
        let target = null;
        try {
          target = await store.read(item.ref);
        } catch (err) {
          if (!(err instanceof InvalidTreeError)) throw err;
        }
        if (!target) {
          out.push({ ...item, _refError: true });
          continue;
        }
        const body = target.tree;
        const resolvedNode = {
          _ref: item.ref,
          _refVersion: target.version,
        };
        if (Array.isArray(body)) {
          const nextVisited = new Set(visited); nextVisited.add(item.ref);
          resolvedNode.children = await resolveRefs(body, nextVisited);
        } else if (body && typeof body === 'object') {
          Object.assign(resolvedNode, body);
          if (Array.isArray(body.children)) {
            const nextVisited = new Set(visited); nextVisited.add(item.ref);
            resolvedNode.children = await resolveRefs(body.children, nextVisited);
          }
        }
        out.push(resolvedNode);
      } else if (Array.isArray(item.children) && item.children.length > 0) {
        out.push({ ...item, children: await resolveRefs(item.children, visited) });
      } else {
        out.push(item);
      }
    }
    return out;
  }

  function stripRefs(items) {
    if (!Array.isArray(items)) return items;
    return items.map(item => {
      if (!item || typeof item !== 'object') return item;
      if (typeof item._ref === 'string') {
        return { ref: item._ref };
      }
      if (Array.isArray(item.children) && item.children.length > 0) {
        return { ...item, children: stripRefs(item.children) };
      }
      return item;
    });
  }

  // GET /tree/:id — read latest version + resolve refs
  router.get('/tree/:id', requireAuth, async (req, res) => {
    try {
      const { id } = req.params;
      const latest = await store.read(id);
      if (!latest) {
        // Never-saved tree — return empty at v0 so consumers don't branch on 404.
        // A subsequent PUT with baseVersion=0 creates v1.
        return res.json({ id, tree: [], version: 0, updatedAt: null });
      }
      const resolved = await resolveRefs(latest.tree);
      res.json({
        id,
        tree: resolved,
        version: latest.version,
        updatedAt: latest.updatedAt,
      });
    } catch (error) {
      if (error instanceof InvalidTreeError) {
        return res.status(400).json({ error: error.message });
      }
      console.error('Error fetching tree:', error);
      res.status(500).json({ error: 'Failed to fetch tree', message: error.message });
    }
  });

  // PUT /tree/:id — create new version; body: { tree, baseVersion }
  router.put('/tree/:id', requireAuth, async (req, res) => {
    try {
      const { id } = req.params;
      const { tree, baseVersion } = req.body;
      if (tree === undefined || tree === null) {
        return res.status(400).json({ error: 'Request body must contain a tree field' });
      }

      const author = req.user?.email || req.user?.name || req.user?.sub || 'unknown';
      const saved = await store.write(id, stripRefs(tree), baseVersion, author);
      res.json({
        id,
        tree: await resolveRefs(saved.tree),
        version: saved.version,
        updatedAt: saved.updatedAt,
      });
    } catch (error) {
      if (error instanceof ConflictError) {
        return res.status(409).json({
          error: 'Conflict detected',
          message: error.message,
          currentTree: error.current ? await resolveRefs(error.current.tree) : [],
          currentVersion: error.current?.version || 0,
        });
      }
      if (error instanceof InvalidTreeError) {
        return res.status(422).json({ error: 'Tree rejected', message: error.message });
      }
      console.error('Error saving tree:', error);
      res.status(500).json({ error: 'Failed to save tree', message: error.message });
    }
  });

  return router;
}
