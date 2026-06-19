# Covers — zero-dependency Node app. No build step, no native modules.
FROM node:20-alpine

WORKDIR /app

# App sources (no dependencies to install)
COPY package.json server.js ./
COPY public ./public

ENV NODE_ENV=production
ENV PORT=8080
ENV DATA_DIR=/data

# Persisted JSON datastore lives here (mount a volume)
VOLUME ["/data"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=4s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null 2>&1 || exit 1

CMD ["node", "server.js"]
