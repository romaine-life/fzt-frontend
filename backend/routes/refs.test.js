import test from 'node:test';
import assert from 'node:assert/strict';
import express from 'express';
import { createFztFrontendRoutes } from './app.js';

async function withServer(trees, run) {
  const writes = [];
  const store = {
    read: async id => trees[id] ?? null,
    write: async (id, tree, version) => { writes.push({ id, tree }); return { tree, version: version + 1 }; },
  };
  const app = express(); app.use(express.json());
  app.use('/fzt', createFztFrontendRoutes({ store, requireAuth: (req, res, next) => { req.user = { sub: 'owner' }; next(); } }));
  const server = app.listen(0, '127.0.0.1');
  await new Promise(resolve => server.once('listening', resolve));
  try {
    await run(`http://127.0.0.1:${server.address().port}/fzt/tree/`, writes);
  } finally { await new Promise(resolve => server.close(resolve)); }
}

const bookmarks = {
  version: 7,
  tree: [
    { name: 'Bills', children: [{ name: 'Ford', url: 'https://ford.example/' }] },
    { ref: 'google' },
    { name: 'dev-environments', children: [] },
  ],
};
const google = { version: 3, tree: { name: 'Google', children: [{ name: 'Gmail', url: 'https://mail.google.com' }] } };

test('a labelled ref expands under its own label and saves back as the same labelled pointer', async () => {
  const label = { name: 'links', description: 'Bookmarks', tags: ['web'] };
  const menu = { version: 1, tree: [{ ref: 'owner-bookmarks', ...label }, { ref: 'google' }] };
  await withServer({ 'owner-menu': menu, 'owner-bookmarks': bookmarks, google }, async (base, writes) => {
    const got = await (await fetch(base + 'owner-menu')).json();
    const [links, plain] = got.tree;
    assert.equal(links.name, 'links');
    assert.equal(links.description, 'Bookmarks');
    assert.deepEqual(links.tags, ['web']);
    assert.deepEqual(links.children.map(c => c.name ?? c.ref), ['Bills', 'Google'], 'the empty dev-environments folder is left out');
    assert.equal(links.children[1].children[0].name, 'Gmail', 'a ref inside the referenced tree expands too');
    assert.equal(plain.name, 'Google', 'an unlabelled ref keeps the referenced tree\'s own name');

    const response = await fetch(base + 'owner-menu', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ tree: got.tree, baseVersion: 1 }) });
    assert.equal(response.status, 200);
    assert.deepEqual(writes[0].tree, menu.tree);
  });
});

test('a ref carrying anything besides a label stays a plain node', async () => {
  const menu = { version: 1, tree: [{ ref: 'owner-bookmarks', name: 'links', url: 'https://x.example/' }] };
  await withServer({ 'owner-menu': menu, 'owner-bookmarks': bookmarks }, async base => {
    const got = await (await fetch(base + 'owner-menu')).json();
    assert.deepEqual(got.tree, menu.tree);
  });
});

test('a referenced tree\'s own empty folders stay in it when it is read directly', async () => {
  await withServer({ 'owner-bookmarks': bookmarks, google }, async base => {
    const got = await (await fetch(base + 'owner-bookmarks')).json();
    assert.equal(got.tree.at(-1).name, 'dev-environments');
  });
});
