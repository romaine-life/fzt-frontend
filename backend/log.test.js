import test from 'node:test';
import assert from 'node:assert/strict';
import express from 'express';
import { parse } from '@romaine-life/logline';
import { errorText, log, outside, OUTSIDE_LIMIT, requestLog } from './log.js';
import { createFztFrontendRoutes } from './routes/app.js';

// The lines the backend writes to stdout while fn runs, each read back with logline's parse:
// a line that doesn't fit the standard fails the test.
async function captured(fn) {
  const lines = [];
  const write = process.stdout.write;
  process.stdout.write = (chunk, ...rest) => {
    const text = String(chunk);
    // node:test's own report goes through stdout too; keep only the backend's lines.
    if (/^\d{4}-\d\d-\d\dT/.test(text)) { lines.push(text); return true; }
    return write.call(process.stdout, chunk, ...rest);
  };
  try { await fn(); } finally { process.stdout.write = write; }
  return lines.join('').split('\n').filter(Boolean).map((line) => {
    const entry = parse(line);
    return { ...entry, fields: Object.fromEntries(entry.fields) };
  });
}

test('log writes standard lines, leaving out absent fields', async () => {
  const [entry] = await captured(() => log.info('fzt-frontend ready', { port: 3000, extra: undefined, none: null }));
  assert.equal(entry.level, 'info');
  assert.equal(entry.message, 'fzt-frontend ready');
  assert.deepEqual(entry.fields, { port: '3000' });
  assert.equal(entry.pid, process.pid);
});

test('outside and errorText mask credentials and cap the text', () => {
  assert.equal(outside('push failed: Authorization: Bearer abc.def'), 'push failed: Authorization: Bearer ***');
  assert.equal(outside('x'.repeat(OUTSIDE_LIMIT + 10)).length, OUTSIDE_LIMIT + 3);
  assert.match(errorText(new Error('password=hunter2')), /^Error: password=\*\*\*\n {4}at /);
  assert.equal(errorText({ message: 'password=hunter2' }), 'password=***');
});

test('one request line per request: full path, no query, caller, 5xx at error', async () => {
  const store = {
    read: async (id) => { if (id === 'broken') throw new Error('git fetch failed: password=hunter2'); return null; },
    write: async () => { throw new Error('unused'); },
  };
  const app = express();
  app.use(requestLog());
  app.use('/fzt', createFztFrontendRoutes({ store, requireAuth: (req, _res, next) => { req.caller = 'home device=mac'; req.user = { sub: 'x' }; next(); } }));
  const server = app.listen(0, '127.0.0.1');
  await new Promise((resolve) => server.once('listening', resolve));
  let entries;
  try {
    const base = `http://127.0.0.1:${server.address().port}`;
    entries = await captured(async () => {
      await fetch(`${base}/fzt/tree/nelson-menu?token=secret-value`);
      await fetch(`${base}/fzt/tree/broken`);
      await fetch(`${base}/nowhere`);
    });
  } finally { await new Promise((resolve) => server.close(resolve)); }
  const requests = entries.filter((e) => e.message === 'request');
  assert.equal(requests.length, 3);
  const [ok, broken, missing] = requests;
  assert.equal(ok.level, 'info');
  assert.deepEqual({ ...ok.fields, ms: undefined }, { method: 'GET', path: '/fzt/tree/nelson-menu', status: '200', ms: undefined, caller: 'home device=mac' });
  assert.match(ok.fields.ms, /^\d+$/);
  assert.equal(broken.level, 'error');
  assert.equal(broken.fields.status, '500');
  assert.equal(missing.fields.status, '404');
  assert.equal(missing.fields.caller, undefined);
  const failed = entries.find((e) => e.message === 'tree read failed');
  assert.equal(failed.level, 'error');
  assert.equal(failed.fields.id, 'broken');
  assert.match(failed.fields.error, /^Error: git fetch failed: password=\*\*\*/);
  for (const entry of entries) assert.doesNotMatch(JSON.stringify(entry.fields), /secret-value|hunter2/);
});
