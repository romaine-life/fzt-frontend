// Backend for fzt-frontend.romaine.life. Hosts the unified tree API
// (/fzt/tree/:id) consumed by my-homepage web and fzt-automate CLI.
// This API only verifies JWTs — it never issues them. Callers sign their
// own JWTs with `api-jwt-signing-secret` from the app Key Vault.
import 'dotenv/config';
import express from 'express';
import helmet from 'helmet';
import morgan from 'morgan';
import cors from 'cors';
import { CosmosClient } from '@azure/cosmos';
import { DefaultAzureCredential } from '@azure/identity';
import { createFztFrontendRoutes } from './routes/index.js';
import { createRequireAuth } from './auth.js';
import { fetchConfig } from './config.js';

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
  const config = await fetchConfig();

  const credential = new DefaultAzureCredential();
  const cosmosClient = new CosmosClient({
    endpoint: config.cosmosDbEndpoint,
    aadCredentials: credential,
  });
  // Tree data lives in HomepageDB.fzt-frontend-data (legacy container name
  // carried forward from pre-tree era — every tree owns its own partition
  // keyed by its id).
  const container = cosmosClient.database('HomepageDB').container('fzt-frontend-data');

  const requireAuth = createRequireAuth({ jwtSecret: config.jwtSigningSecret });

  app.use('/fzt', createFztFrontendRoutes({ requireAuth, container }));

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
