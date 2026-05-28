import { createRemoteJWKSet, jwtVerify } from 'jose';

const AUTH_ISSUER = process.env.AUTH_ISSUER || 'https://auth.romaine.life';
const AUTH_JWKS_URL = process.env.AUTH_JWKS_URL || `${AUTH_ISSUER}/api/auth/jwks`;
const AUTH_ROLES = new Set(['admin', 'user', 'service']);

// fzt-frontend only verifies JWTs — it never issues them. All callers
// present auth.romaine.life RS256 tokens, verified against the issuer's
// JWKS. The legacy HS256 api-jwt-signing-secret path was removed in the
// auth.romaine.life migration — no compatibility fallback.
export function createRequireAuth() {
  const authJwks = createRemoteJWKSet(new URL(AUTH_JWKS_URL));

  return async (req, res, next) => {
    let token;
    const authHeader = req.headers.authorization;
    if (authHeader?.startsWith('Bearer ')) {
      token = authHeader.slice(7);
    } else {
      const cookies = req.headers.cookie || '';
      const match = cookies.split(';').map(c => c.trim()).find(c => c.startsWith('auth_token='));
      if (match) token = match.slice('auth_token='.length);
    }

    if (!token) {
      return res.status(401).json({ error: 'Missing authentication' });
    }

    try {
      const { payload } = await jwtVerify(token, authJwks, {
        issuer: AUTH_ISSUER,
      });
      const role = typeof payload.role === 'string' ? payload.role : 'user';
      if (!AUTH_ROLES.has(role)) {
        return res.status(403).json({ error: 'Forbidden' });
      }
      req.user = {
        sub: payload.sub,
        email: payload.email,
        name: payload.name,
        role,
        apps: payload.apps || {},
      };
      return next();
    } catch {
      return res.status(401).json({ error: 'Invalid or expired token' });
    }
  };
}
