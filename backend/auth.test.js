import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import express from 'express';
import { generateKeyPair, exportJWK, SignJWT } from 'jose';

let keys, jwks, api, base;
before(async () => {
  keys = await generateKeyPair('RS256');
  const publicKey = { ...await exportJWK(keys.publicKey), kid: 'test', alg: 'RS256' };
  jwks = http.createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ keys: [publicKey] }));
  });
  await new Promise(resolve => jwks.listen(0, '127.0.0.1', resolve));
  process.env.AUTH_JWKS_URL = `http://127.0.0.1:${jwks.address().port}/jwks`;
  const { createRequireAuth } = await import('./auth.js');
  const app = express();
  app.get('/tree', createRequireAuth(), (req, res) => res.json(req.user));
  api = app.listen(0, '127.0.0.1');
  await new Promise(resolve => api.once('listening', resolve));
  base = `http://127.0.0.1:${api.address().port}/tree`;
});
after(async () => {
  await Promise.all([api, jwks].map(server => new Promise(resolve => server.close(resolve))));
});

async function token(role = 'user', issuer = 'https://auth.romaine.life', expiration = '5m') {
  return new SignJWT({ role, email: 'fixture@example.com' }).setProtectedHeader({ alg: 'RS256', kid: 'test' })
    .setIssuer(issuer).setSubject('fixture-user').setIssuedAt().setExpirationTime(expiration).sign(keys.privateKey);
}

test('identity-provider RS256 tokens and the existing cookie fallback work', async () => {
  for (const role of ['admin', 'user', 'service']) {
    const res = await fetch(base, { headers: { Authorization: `Bearer ${await token(role)}` } });
    assert.equal(res.status, 200);
    assert.equal((await res.json()).sub, 'fixture-user');
  }
  assert.equal((await fetch(base, { headers: { Cookie: `auth_token=${await token()}` } })).status, 200);
});

test('missing, expired, tampered and wrong-issuer tokens remain rejected', async () => {
  assert.equal((await fetch(base)).status, 401);
  for (const jwt of [await token('user', 'https://other.example'), await token('user', undefined, Math.floor(Date.now() / 1000) - 60)]) {
    assert.equal((await fetch(base, { headers: { Authorization: `Bearer ${jwt}` } })).status, 401);
  }
  const jwt = await token();
  const parts = jwt.split('.');
  parts[1] = Buffer.from(JSON.stringify({ sub: 'attacker', role: 'admin' })).toString('base64url');
  assert.equal((await fetch(base, { headers: { Authorization: `Bearer ${parts.join('.')}` } })).status, 401);
});

test('a signed role outside the existing role set remains forbidden', async () => {
  assert.equal((await fetch(base, { headers: { Authorization: `Bearer ${await token('outsider')}` } })).status, 403);
});
