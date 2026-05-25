# fzt-frontend

Canonical backend for the unified tree/bookmark stack. Hosts a
versioned, ref-resolving tree API that every UI in the romaine.life
ecosystem reads from and writes to.

## Container Build Verification

Agent pods are not expected to have Docker. Do not report missing local Docker
as a blocker. Run available repo checks first, then use PR CI as the normal
container build gate: `.github/workflows/docker-build-check.yml` performs a
throwaway Docker build with `push: false`. If image-packaging feedback is
needed before a PR is ready, manually dispatch that workflow with `git_ref`.
Release/deploy workflows are the only path that publishes images.

## Where this fits

```
my-homepage           romaine-api.py            (any browser tab,
(static SPA at         CLI on a dev box;          terminal, or
 homepage.romaine     mints its own JWT          script with a
 .life)               for any identity)          fresh JWT)
       \                    |                          /
        \                   |                         /
         v                  v                        v
       ┌────────────────────────────────────────────────┐
       │ fzt-frontend (this repo) at                    │
       │ fzt-frontend.romaine.life                      │
       │   • verifies JWTs (does NOT issue them)        │
       │   • CRUD over /fzt/tree/:id                    │
       │   • resolves {ref:"…"} pointers transparently  │
       └────────────────────────────────────────────────┘
                             │
                             v
                     Cosmos: HomepageDB.fzt-frontend-data
                     (partition key /userId, one tree per partition)
```

my-homepage is a static frontend — it owns no data. Anything that wants
to read or write a tree (bookmarks, menus, anything else tree-shaped)
talks to this backend.

## API surface

Mounted at the **`/fzt`** prefix (look at `backend/server.js:50` —
nothing else points to it; missing this is the most common 404 when
calling the API for the first time).

| Method | Path                | Body                                | Response                                                 |
|--------|---------------------|-------------------------------------|----------------------------------------------------------|
| GET    | `/fzt/tree/:id`     | —                                   | `{ id, tree, version, updatedAt }` with refs resolved    |
| PUT    | `/fzt/tree/:id`     | `{ tree, baseVersion }`             | `{ id, tree, version, updatedAt }` after writing v(N+1) |

A never-saved tree GETs as `{ tree: [], version: 0, updatedAt: null }`
so callers don't have to special-case 404.

PUTs are version-checked: pass `baseVersion` equal to the version you
read; the server returns 409 if it has moved on, with the current
state attached so callers can merge and retry.

## Auth model

This backend **only verifies** — it never issues tokens. Browser callers use
RS256 JWTs issued by `auth.romaine.life` and verified against
`https://auth.romaine.life/api/auth/jwks` with issuer
`https://auth.romaine.life`. The legacy HS256 `api-jwt-signing-secret` path
from the app-owned `ng6-fzt-frontend` Key Vault remains for existing
terminal/CLI callers during migration.

Bearer header preferred; legacy cookie `auth_token=` accepted as fallback.
Identity claims (`sub`, `email`, `name`, `role`) are baked into the token
payload — there is **no per-tree ACL**. Any authenticated caller can read or
write any tree id. Identity-based scoping is enforced client-side by choosing
which tree ids to fetch and save (e.g. `nelson-bookmarks` vs
`nelson-ea-bookmarks`).

Adding an identity: edit `claims.go` in this repo *and* the
`IDENTITIES` dict in `romaine-api.py` — the duplication is intentional
(one Go consumer, one Python consumer, both self-contained).

## Cosmos schema

Database `HomepageDB`, container `fzt-frontend-data` (legacy name from
the pre-tree era — every tree owns its own partition keyed by its id).

Each PUT writes a new doc — versions are append-only:

```js
{
  id:       `tree_${treeId}_v${version}`,
  userId:   treeId,        // partition key path is /userId
  treeId:   treeId,
  type:     'tree',
  version:  N,
  tree:     <array or object>,
  updatedAt: ISO8601 string,
}
```

GETs `SELECT TOP 1 ... ORDER BY c.version DESC` — old versions stay
readable directly by id but the API only ever surfaces the latest.

## Tree refs

Anywhere inside a tree, an item shaped exactly `{ ref: "<otherTreeId>" }`
expands inline on GET — the resolver walks the referenced tree's body
in place, tagging the resolved node with `_ref` and `_refVersion` so a
subsequent PUT can collapse it back to pointer form. Cycle-guarded
(visited set) and depth-limited (`MAX_REF_DEPTH=10`).

Cross-tree writes are **not** performed: PUT only writes the target
tree id, and `_ref` markers in the body are stripped back to
`{ ref: ... }`. To edit a referenced sub-tree, PUT that tree's id
directly.

## Updating a tree programmatically

```sh
# Read
romaine-api.py tree-get nelson-bookmarks > tree.json

# Edit tree.json with $EDITOR or jq

# Write — picks up the current version automatically; pass
# --base-version to pin against concurrent edits
romaine-api.py tree-put nelson-bookmarks --from tree.json
```

Or do it by hand:

```sh
TOKEN=$(romaine-api.py mint-token --identity nelson)
curl -H "Authorization: Bearer $TOKEN" \
     https://fzt-frontend.romaine.life/fzt/tree/nelson-bookmarks
```

## Local dev

```sh
cd backend
npm install
PORT=3000 node server.js
# server reads cosmosDbEndpoint + jwtSigningSecret via DefaultAzureCredential —
# `az login` against a principal with read access on ng6-fzt-frontend first.
```

Frontend dev (Go-WASM fzt terminal): see `fzt-terminal/CLAUDE.md` for
the keyboard model and rendering pipeline; this repo only ships the
backend Go process (cmd/wasm/main.go is the WASM bridge that
`my-homepage` and others embed).
