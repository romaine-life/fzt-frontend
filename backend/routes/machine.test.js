import test from 'node:test';
import assert from 'node:assert/strict';
import express from 'express';
import { createFztFrontendRoutes } from './app.js';

test('a machine writes only its granted personal trees and keeps its own audit identity', async () => {
  const writes = [];
  const actor = { sub: 'device:owner/mac', name: 'Machine: mac', email: 'mac@service.machine.romaine.life', purpose: 'machine', apps: { fzt: { owner_sub: 'owner' } } };
  const store = { read: async () => null, write: async (id, tree, version, author) => { writes.push({ id, author }); return { tree, version: version + 1 }; } };
  const app = express(); app.use(express.json());
  app.use('/fzt', createFztFrontendRoutes({ store, requireAuth: (req, res, next) => { req.user = actor; next(); } }));
  const server = app.listen(0, '127.0.0.1');
  await new Promise(resolve => server.once('listening', resolve));
  try {
    const base = `http://127.0.0.1:${server.address().port}/fzt/tree/`;
    for (const [tree, status] of [['owner-menu', 200], ['owner-bookmarks', 200], ['someone-else-menu', 403], ['shared', 403]]) {
      const response = await fetch(base + tree, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ tree: [], baseVersion: 0 }) });
      assert.equal(response.status, status);
    }
    assert.equal(writes.length, 2);
    assert.deepEqual(writes[0].author, { name: actor.name, email: actor.email, sub: actor.sub });
  } finally { await new Promise(resolve => server.close(resolve)); }
});
