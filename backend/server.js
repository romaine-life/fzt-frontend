// Backend for fzt-frontend.home.romaine.life. Hosts the unified tree API
// (/fzt/tree/:id) consumed by my-homepage web and fzt-automate CLI.
// This API only verifies JWTs — it never issues them (see auth.js).
import 'dotenv/config';
import express from 'express';
import helmet from 'helmet';
import morgan from 'morgan';
import cors from 'cors';
import { readFile } from 'node:fs/promises';
import { createFztFrontendRoutes } from './routes/index.js';
import { createRequireAuth } from './auth.js';
import { Store } from './store.js';

const app = express();
const PORT = process.env.PORT || 3000;
let serverReady = false;

app.use(helmet({ contentSecurityPolicy: false }));
app.use(cors({ origin: true, credentials: true }));
app.use(express.json({ limit: '10mb' }));
// Which kind of token each request used, so the machines' move to home tokens reads from the log.
morgan.token('token-kind', req => req.caller || '-');
app.use(morgan(`${morgan.combined} caller=":token-kind"`));

app.use((req, res, next) => {
  if (serverReady || req.path === '/health') return next();
  res.status(503).json({ error: 'Starting' });
});

app.get('/health', (req, res) => {
  if (!serverReady) return res.status(503).json({ status: 'starting' });
  res.json({ status: 'healthy' });
});

async function start() {
  // homesvc reads this repo-scoped credential from OpenBao as the NAS device
  // and mounts it read-only. JWT verification below is unchanged.
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
  console.log(`[fzt-frontend] ready on port ${PORT}`);
}

app.listen(PORT, () => {
  start().catch((err) => {
    console.error('[fzt-frontend] fatal startup error:', err);
    process.exit(1);
  });
});

export default app;
