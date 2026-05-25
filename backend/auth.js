import jwt from 'jsonwebtoken';

// fzt-frontend only verifies JWTs — callers (my-homepage web, fzt-automate
// CLI) sign their own with the app-vault `api-jwt-signing-secret`. No token
// issuance endpoints live on this backend.
export function createRequireAuth({ jwtSecret }) {
  return (req, res, next) => {
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
      const payload = jwt.verify(token, jwtSecret);
      req.user = {
        sub: payload.sub,
        email: payload.email,
        name: payload.name,
        role: payload.role || 'member',
      };
      next();
    } catch {
      return res.status(401).json({ error: 'Invalid or expired token' });
    }
  };
}
