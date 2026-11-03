FROM node:20.19.5-alpine3.22

# The tree store is a git clone of romaine-life/at, reached over SSH.
RUN apk add --no-cache git openssh-client

WORKDIR /app

RUN addgroup -g 1001 -S nodejs && \
    adduser -S nodejs -u 1001 && \
    chown nodejs:nodejs /app
USER nodejs

COPY --chown=nodejs:nodejs backend/package*.json backend/
RUN cd backend && npm ci --omit=dev
COPY --chown=nodejs:nodejs backend/ backend/

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD node -e "require('http').get('http://localhost:3000/health', (r) => {process.exit(r.statusCode === 200 ? 0 : 1)})"

CMD ["node", "backend/server.js"]
