import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import express from 'express';
import { generateKeyPair, exportJWK, SignJWT } from 'jose';

const HOME_ISSUER = 'https://bao.home.romaine.life/v1/identity/oidc';

let keys, homeKeys, jwks, homeJwks, authKeyReads = 0, api, base;
before(async () => {
  keys = await generateKeyPair('RS256');
  homeKeys = await generateKeyPair('ES256');
  const publicKey = { ...await exportJWK(keys.publicKey), kid: 'test', alg: 'RS256' };
  jwks = http.createServer((req, res) => {
    authKeyReads += 1;
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ keys: [publicKey] }));
  });
  await new Promise(resolve => jwks.listen(0, '127.0.0.1', resolve));
  process.env.AUTH_JWKS_URL = `http://127.0.0.1:${jwks.address().port}/jwks`;
  const homeKey = { ...await exportJWK(homeKeys.publicKey), kid: 'home', alg: 'ES256' };
  homeJwks = http.createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ keys: [homeKey] }));
  });
  await new Promise(resolve => homeJwks.listen(0, '127.0.0.1', resolve));
  process.env.HOME_JWKS_URL = `http://127.0.0.1:${homeJwks.address().port}/keys`;
  const { createRequireAuth } = await import('./auth.js');
  const app = express();
  app.get('/tree', createRequireAuth(), (req, res) => res.json({ ...req.user, caller: req.caller }));
  api = app.listen(0, '127.0.0.1');
  await new Promise(resolve => api.once('listening', resolve));
  base = `http://127.0.0.1:${api.address().port}/tree`;
});
after(async () => {
  await Promise.all([api, jwks, homeJwks].map(server => new Promise(resolve => server.close(resolve))));
});

async function token(role = 'user', issuer = 'https://auth.romaine.life', expiration = '5m') {
  return new SignJWT({ role, email: 'fixture@example.com' }).setProtectedHeader({ alg: 'RS256', kid: 'test' })
    .setAudience('https://auth.romaine.life').setIssuer(issuer).setSubject('fixture-user').setIssuedAt().setExpirationTime(expiration).sign(keys.privateKey);
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

test('federation tokens and legacy device-as-human credentials cannot bypass machine permissions', async () => {
  for (const payload of [
    { sub: 'device:owner/mac', aud: 'openbao' },
    { sub: 'owner', role: 'admin', email: 'owner@example.com', device: 'mac' },
  ]) {
    const jwt = await new SignJWT(payload).setProtectedHeader({ alg: 'RS256', kid: 'test' })
      .setIssuer('https://auth.romaine.life').setAudience(payload.aud || 'https://auth.romaine.life').setIssuedAt().setExpirationTime('5m').sign(keys.privateKey);
    assert.equal((await fetch(base, { headers: { Authorization: `Bearer ${jwt}` } })).status, 401);
  }
});

function homeToken({ claims = { device: 'mac', apps: { fzt: { owner_sub: 'owner' } } }, issuer = HOME_ISSUER, audience = 'fzt-frontend', expiration = '5m' } = {}) {
  return new SignJWT(claims).setProtectedHeader({ alg: 'ES256', kid: 'home' })
    .setIssuer(issuer).setAudience(audience).setSubject('entity-uuid').setIssuedAt().setExpirationTime(expiration).sign(homeKeys.privateKey);
}
const call = jwt => fetch(base, { headers: { Authorization: `Bearer ${jwt}` } });

test('a home token signs in as its device, with its owner grant', async () => {
  const res = await call(await homeToken());
  assert.equal(res.status, 200);
  assert.deepEqual(await res.json(), {
    sub: 'device:mac', email: 'mac@machine.home.romaine.life', name: 'Machine: mac',
    role: 'service', purpose: 'machine', apps: { fzt: { owner_sub: 'owner' } }, caller: 'home device=mac',
  });
});

test('a home token never reads auth.romaine.life keys', async () => {
  const { createRequireAuth } = await import('./auth.js');
  const fresh = express();
  fresh.get('/tree', createRequireAuth(), (req, res) => res.json(req.user));
  const server = fresh.listen(0, '127.0.0.1');
  await new Promise(resolve => server.once('listening', resolve));
  try {
    const reads = authKeyReads;
    const res = await fetch(`http://127.0.0.1:${server.address().port}/tree`, { headers: { Authorization: `Bearer ${await homeToken()}` } });
    assert.equal(res.status, 200);
    assert.equal(authKeyReads, reads);
  } finally { await new Promise(resolve => server.close(resolve)); }
});

test('an RS256 token claiming the home issuer is refused', async () => {
  const signedByAuth = await new SignJWT({ device: 'mac' }).setProtectedHeader({ alg: 'RS256', kid: 'test' })
    .setIssuer(HOME_ISSUER).setAudience('fzt-frontend').setIssuedAt().setExpirationTime('5m').sign(keys.privateKey);
  assert.equal((await call(signedByAuth)).status, 401);
});

test('wrong audience, issuer, expiry, missing device and alg none are refused', async () => {
  const none = (payload) => [{ alg: 'none' }, payload].map(part => Buffer.from(JSON.stringify(part)).toString('base64url')).join('.') + '.';
  const now = Math.floor(Date.now() / 1000);
  for (const jwt of [
    await homeToken({ audience: 'shows' }),
    await homeToken({ audience: 'session-index' }),
    await homeToken({ issuer: 'https://bao.home.romaine.life.example/v1/identity/oidc' }),
    await homeToken({ expiration: now - 60 }),
    await homeToken({ claims: {} }),
    await homeToken({ claims: { device: '' } }),
    none({ iss: HOME_ISSUER, aud: 'fzt-frontend', device: 'mac', iat: now, exp: now + 300 }),
    'not-a-jwt',
  ]) {
    assert.equal((await call(jwt)).status, 401);
  }
});

test('the caller names the token kind for the log', async () => {
  const signed = (payload) => new SignJWT({ email: 'x@example.com', ...payload }).setProtectedHeader({ alg: 'RS256', kid: 'test' })
    .setIssuer('https://auth.romaine.life').setAudience('https://auth.romaine.life').setSubject('fixture-user').setIssuedAt().setExpirationTime('5m').sign(keys.privateKey);
  for (const [payload, caller] of [
    [{ role: 'admin' }, 'auth human sub=fixture-user'],
    [{ role: 'service', purpose: 'machine', device: 'mac' }, 'auth machine device=mac'],
    [{ role: 'service' }, 'auth service sub=fixture-user'],
  ]) {
    const res = await call(await signed(payload));
    assert.equal(res.status, 200);
    assert.equal((await res.json()).caller, caller);
  }
});
