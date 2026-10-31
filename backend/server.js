// Backend for fzt-frontend.romaine.life. Hosts the unified tree API
// (/fzt/tree/:id) consumed by my-homepage web and fzt-automate CLI.
// This API only verifies JWTs — it never issues them. All callers present
// auth.romaine.life RS256 tokens, verified against the issuer's JWKS.
import 'dotenv/config';
import express from 'express';
import helmet from 'helmet';
import morgan from 'morgan';
import cors from 'cors';
import { DefaultAzureCredential } from '@azure/identity';
import { SecretClient } from '@azure/keyvault-secrets';
import { createFztFrontendRoutes } from './routes/index.js';
import { createRequireAuth } from './auth.js';
import { Store } from './store.js';

const app = express();
const PORT = process.env.PORT || 3000;
let serverReady = false;

app.use(helmet({ contentSecurityPolicy: false }));
app.use(cors({ origin: true, credentials: true }));
app.use(express.json({ limit: '10mb' }));
app.use(morgan('combined'));

app.use((req, res, next) => {
  if (serverReady || req.path === '/health') return next();
  res.status(503).json({ error: 'Starting' });
});

app.get('/health', (req, res) => {
  if (!serverReady) return res.status(503).json({ status: 'starting' });
  res.json({ status: 'healthy' });
});

async function start() {
  // Trees live in the romaine-life/at repo; the backend pushes with a deploy key
  // scoped to that one repo, kept in this app's Key Vault.
  const vault = new SecretClient(process.env.KEY_VAULT_URL, new DefaultAzureCredential());
  const { value: deployKey } = await vault.getSecret('at-store-deploy-key');
  const store = await Store.open({
    repoUrl: process.env.STORE_REPO || 'git@github.com:romaine-life/at.git',
    dir: process.env.STORE_DIR || '/app/store',
    deployKey,
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
