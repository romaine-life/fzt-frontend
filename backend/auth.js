import { createRemoteJWKSet, decodeJwt, jwtVerify } from 'jose';

const AUTH_ISSUER = process.env.AUTH_ISSUER || 'https://auth.romaine.life';
const AUTH_JWKS_URL = process.env.AUTH_JWKS_URL || `${AUTH_ISSUER}/api/auth/jwks`;
const AUTH_ROLES = new Set(['admin', 'user', 'service']);
const HOME_ISSUER = 'https://bao.home.romaine.life/v1/identity/oidc';
const HOME_JWKS_URL = process.env.HOME_JWKS_URL || `${HOME_ISSUER}/.well-known/keys`;
const HOME_AUDIENCE = 'fzt-frontend';

function issuerOf(token) {
  try {
    return decodeJwt(token).iss;
  } catch {
    return undefined;
  }
}

function authCaller(payload) {
  if (payload.purpose === 'machine') return `auth machine device=${payload.device}`;
  return `auth ${payload.role === 'service' ? 'service' : 'human'} sub=${payload.sub}`;
}

// fzt-frontend only verifies JWTs — it never issues them. Browsers present
// auth.romaine.life RS256 tokens; home machines present OpenBao ES256 tokens,
// checked against OpenBao's keys alone. The legacy HS256 api-jwt-signing-secret
// path was removed in the auth.romaine.life migration — no compatibility fallback.
export function createRequireAuth() {
  const authJwks = createRemoteJWKSet(new URL(AUTH_JWKS_URL));
  const homeJwks = createRemoteJWKSet(new URL(HOME_JWKS_URL));

  async function verifyHome(req, res, next, token) {
    try {
      const { payload } = await jwtVerify(token, homeJwks, {
        issuer: HOME_ISSUER,
        audience: HOME_AUDIENCE,
        algorithms: ['ES256'],
        requiredClaims: ['iat', 'exp'],
      });
      const { device } = payload;
      if (typeof device !== 'string' || !device) {
        return res.status(401).json({ error: 'Invalid or expired token' });
      }
      req.user = {
        sub: `device:${device}`,
        email: `${device}@machine.home.romaine.life`,
        name: `Machine: ${device}`,
        role: 'service',
        purpose: 'machine',
        apps: payload.apps || {},
      };
      req.caller = `home device=${device}`;
      return next();
    } catch {
      return res.status(401).json({ error: 'Invalid or expired token' });
    }
  }

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

    const issuer = issuerOf(token);
    if (issuer === HOME_ISSUER) return verifyHome(req, res, next, token);
    if (issuer !== AUTH_ISSUER) {
      return res.status(401).json({ error: 'Invalid or expired token' });
    }

    try {
      const { payload } = await jwtVerify(token, authJwks, {
        issuer: AUTH_ISSUER,
        audience: AUTH_ISSUER,
        requiredClaims: ['sub', 'email', 'role', 'iat', 'exp'],
      });
      const role = payload.role;
      if (!AUTH_ROLES.has(role)) {
        return res.status(403).json({ error: 'Forbidden' });
      }
      if (payload.device && payload.purpose !== 'machine') {
        return res.status(401).json({ error: 'Legacy device-as-human token; renew as the machine' });
      }
      req.user = {
        sub: payload.sub,
        email: payload.email,
        name: payload.name,
        role,
        purpose: payload.purpose,
        apps: payload.apps || {},
      };
      req.caller = authCaller(payload);
      return next();
    } catch {
      return res.status(401).json({ error: 'Invalid or expired token' });
    }
  };
}
