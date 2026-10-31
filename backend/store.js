// The tree store: a clone of romaine-life/at. A tree is trees/<id>.json, a write is a
// commit pushed straight to main, and git's push is the arbiter between writers
// (another pod mid-rollout, or a merged PR). Every git operation runs one at a time.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';

const exec = promisify(execFile);

// GitHub's published host key, pinned so the deploy key is only ever offered to GitHub.
const GITHUB_HOST_KEY =
  'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n';
const FRESH_MS = 5000;
const PUSH_ATTEMPTS = 3;
const TREE_ID = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export class ConflictError extends Error {
  constructor(current) {
    super('Tree has been modified elsewhere.');
    this.current = current;
  }
}

export class InvalidTreeError extends Error {}

function isUrl(text) {
  return text.includes('://') || /^(https?|mailto):/.test(text);
}

// The command a menu node runs, or null for folders, URLs and refs. Matches
// tools/check.py in the store, which CI runs on PRs.
function commandOf(node) {
  if ('ref' in node) return null;
  if (node.action) {
    if (isUrl(node.action)) return null;
    return node.action.replace(/^folder:/, '').split(/\s+/)[0];
  }
  if ((node.children && node.children.length) || node.url) return null;
  return node.name || null;
}

function* menuCommands(tree, where = '') {
  if (!Array.isArray(tree)) return;
  for (const node of tree) {
    if (!node || typeof node !== 'object') continue;
    const here = `${where}/${node.name ?? '?'}`;
    const cmd = commandOf(node);
    if (cmd) yield [cmd, here];
    yield* menuCommands(node.children, here);
  }
}

export class Store {
  constructor(dir, env) {
    this.dir = dir;
    this.env = env;
    this.fetchedAt = 0;
    this.queue = Promise.resolve();
  }

  // repoUrl is an SSH URL when deployKey is given, otherwise any URL git can push to.
  static async open({ repoUrl, dir, deployKey }) {
    const env = { ...process.env, GIT_TERMINAL_PROMPT: '0' };
    if (deployKey) {
      const sshDir = `${dir}.ssh`;
      await mkdir(sshDir, { recursive: true });
      const keyFile = path.join(sshDir, 'deploy_key');
      const hostsFile = path.join(sshDir, 'known_hosts');
      await writeFile(keyFile, deployKey.endsWith('\n') ? deployKey : `${deployKey}\n`, { mode: 0o600 });
      await writeFile(hostsFile, GITHUB_HOST_KEY);
      // ssh parses this command line like a shell: quote the paths, and give it forward slashes.
      const q = p => `"${p.replace(/\\/g, '/')}"`;
      env.GIT_SSH_COMMAND = `ssh -i ${q(keyFile)} -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=${q(hostsFile)}`;
    }
    await rm(dir, { recursive: true, force: true });
    await exec('git', ['clone', '-q', repoUrl, dir], { env });
    const store = new Store(dir, env);
    await store.git('config', 'user.name', 'fzt-frontend');
    await store.git('config', 'user.email', 'fzt-frontend@romaine.life');
    store.fetchedAt = Date.now();
    return store;
  }

  git(...args) {
    return exec('git', ['-C', this.dir, ...args], { env: this.env });
  }

  exclusive(fn) {
    const result = this.queue.then(fn, fn);
    this.queue = result.catch(() => {});
    return result;
  }

  async sync(force = false) {
    if (!force && Date.now() - this.fetchedAt < FRESH_MS) return;
    await this.git('fetch', '-q', 'origin', 'main');
    await this.git('reset', '-q', '--hard', 'origin/main');
    this.fetchedAt = Date.now();
  }

  treeFile(id) {
    if (!TREE_ID.test(id)) throw new InvalidTreeError(`'${id}' is not a valid tree id`);
    return path.join(this.dir, 'trees', `${id}.json`);
  }

  async readTree(id) {
    try {
      return JSON.parse(await readFile(this.treeFile(id), 'utf8'));
    } catch (err) {
      if (err.code === 'ENOENT') return null;
      throw err;
    }
  }

  read(id) {
    return this.exclusive(async () => {
      await this.sync();
      return this.readTree(id);
    });
  }

  // A menu may only run commands the store has, and every command must keep an entry.
  async checkMenu(tree) {
    const have = new Set((await readdir(path.join(this.dir, 'commands')))
      .filter(f => f.endsWith('.ps1')).map(f => f.slice(0, -4)));
    const used = new Set();
    const problems = [];
    for (const [cmd, where] of menuCommands(tree)) {
      used.add(cmd);
      if (!have.has(cmd)) problems.push(`${where} runs '${cmd}', which is not a command in the store`);
    }
    for (const cmd of have) {
      if (!used.has(cmd)) problems.push(`'${cmd}' would have no menu entry`);
    }
    if (problems.length) throw new InvalidTreeError(problems.join('; '));
  }

  // Saves tree as the next version of id. baseVersion, when given, must be the current
  // version, or ConflictError carries the current tree instead.
  write(id, tree, baseVersion, author) {
    return this.exclusive(async () => {
      for (let attempt = 1; ; attempt++) {
        await this.sync(true);
        const current = await this.readTree(id);
        const currentVersion = current?.version || 0;
        if (baseVersion !== undefined && baseVersion !== null && baseVersion !== currentVersion) {
          throw new ConflictError(current);
        }
        if (id.endsWith('-menu')) await this.checkMenu(tree);
        const doc = { version: currentVersion + 1, updatedAt: new Date().toISOString(), tree };
        await mkdir(path.join(this.dir, 'trees'), { recursive: true });
        await writeFile(this.treeFile(id), `${JSON.stringify(doc, null, 2)}\n`);
        await this.git('add', `trees/${id}.json`);
        await this.git('commit', '-q', '-m', `Update ${id} to v${doc.version}`, '-m', `Saved through fzt-frontend by ${author}.`);
        try {
          await this.git('push', '-q', 'origin', 'HEAD:main');
          this.fetchedAt = Date.now();
          return doc;
        } catch (err) {
          // Someone else pushed first: start again from their main.
          if (attempt >= PUSH_ATTEMPTS) throw err;
        }
      }
    });
  }
}
