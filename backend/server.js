// Backend for fzt-frontend.home.romaine.life. Hosts the unified tree API
// (/fzt/tree/:id) consumed by my-homepage web and fzt-automate CLI.
// This API only verifies JWTs — it never issues them (see auth.js).
import 'dotenv/config';
import express from 'express';
import helmet from 'helmet';
import cors from 'cors';
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { createFztFrontendRoutes } from './routes/index.js';
import { createRequireAuth } from './auth.js';
import { Store } from './store.js';
import { errorText, log, requestLog } from './log.js';

const app = express();
const PORT = process.env.PORT || 3000;
let serverReady = false;

app.use(helmet({ contentSecurityPolicy: false }));
app.use(cors({ origin: true, credentials: true }));
app.use(express.json({ limit: '10mb' }));
// One line per request, with which kind of token it used (log.js).
app.use(requestLog());

app.use((req, res, next) => {
  if (serverReady || req.path === '/health') return next();
  res.status(503).json({ error: 'Starting' });
});

app.get('/health', (req, res) => {
  if (!serverReady) return res.status(503).json({ status: 'starting' });
  res.json({ status: 'healthy' });
});

async function start() {
  // The repo-scoped deploy key, mounted read-only from OpenBao (k8s/secrets.yaml).
  const deployKey = process.env.STORE_DEPLOY_KEY_FILE
    ? await readFile(process.env.STORE_DEPLOY_KEY_FILE, 'utf8') : undefined;
  const knownHosts = await readFile(new URL('./forgejo_known_hosts', import.meta.url), 'utf8');
  const store = await Store.open({
    repoUrl: process.env.STORE_REPO || 'ssh://git@forgejo.romaine.life:2222/romaine-life/at.git',
    dir: process.env.STORE_DIR || '/app/store',
    deployKey,
    knownHosts,
  });

  const requireAuth = createRequireAuth();

  app.use('/fzt', createFztFrontendRoutes({ requireAuth, store }));

  serverReady = true;
  log.info('fzt-frontend ready', { port: PORT });
}

// The image's self-test (`--self-test`, run by the cluster suite's image check on a
// read-only container): the real server on a throwaway git store, healthy, and refusing a
// tree read with no token or a bad one. Scratch goes in /dev/shm, the one writable place.
async function selfTest() {
  const exec = promisify(execFile);
  const dir = await mkdtemp(path.join(process.env.SELF_TEST_DIR || '/dev/shm', 'fzt-self-test-'));
  try {
    const seed = path.join(dir, 'seed');
    await exec('git', ['init', '-q', '-b', 'main', seed]);
    await writeFile(path.join(seed, 'README'), 'fixture\n');
    await exec('git', ['-C', seed, 'add', 'README']);
    await exec('git', ['-C', seed, '-c', 'user.name=fixture', '-c', 'user.email=fixture@example.com', 'commit', '-qm', 'fixture']);
    process.env.STORE_REPO = seed;
    process.env.STORE_DIR = path.join(dir, 'store');
    delete process.env.STORE_DEPLOY_KEY_FILE;
    const server = await new Promise((resolve) => { const s = app.listen(0, '127.0.0.1', () => resolve(s)); });
    try {
      await start();
      const base = `http://127.0.0.1:${server.address().port}`;
      const expect = async (what, want, ...request) => {
        const { status } = await fetch(...request);
        if (status !== want) throw new Error(`${what}: HTTP ${status}, expected ${want}`);
      };
      await expect('health', 200, `${base}/health`);
      await expect('a tree read with no token', 401, `${base}/fzt/tree/fixture`);
      await expect('a tree read with a bad token', 401, `${base}/fzt/tree/fixture`, { headers: { Authorization: 'Bearer invalid' } });
    } finally {
      server.closeAllConnections();
      server.close();
    }
    log.info('fzt-frontend self-test passed');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

if (process.argv.includes('--self-test')) {
  // Its result is the exit code; nothing is left open, so the process then ends.
  selfTest().catch((err) => {
    log.error('fzt-frontend self-test failed', { error: errorText(err) });
    process.exitCode = 1;
  });
} else {
  app.listen(PORT, () => {
    start().catch((err) => {
      log.error('fzt-frontend startup failed', { error: errorText(err) });
      process.exit(1);
    });
  });
}

export default app;
