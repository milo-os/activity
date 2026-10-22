# Build stage
FROM node:26.12.1-alpine3.24 AS builder

# Install pnpm
RUN npm install -g pnpm@10.29.3

WORKDIR /app

# Copy workspace and lock files first for better caching
COPY pnpm-workspace.yaml ./
COPY pnpm-lock.yaml ./
COPY package.json ./

# Copy library config files
COPY tsconfig.json ./
COPY rollup.config.mjs ./

# Copy source files for the library
COPY src/ ./src/

# Copy example app files
COPY example/package.json ./example/
COPY example/app/ ./example/app/
COPY example/public/ ./example/public/
COPY example/tsconfig.json ./example/
COPY example/vite.config.ts ./example/
COPY example/tailwind.config.js ./example/
COPY example/postcss.config.js ./example/

# Install dependencies and build
RUN pnpm install --frozen-lockfile && \
    pnpm --filter @datum-cloud/activity-ui build && \
    pnpm --filter activity-ui-example build && \
    pnpm deploy --filter activity-ui-example --prod --legacy /deploy && \
    cp -r example/build /deploy/build

# Runtime stage - Node.js for Remix server
FROM node:26.12.1-alpine3.24 AS runtime

RUN apk add --no-cache --upgrade 'ca-certificates>=20261020-r0'
RUN ! grep -q MIICTjCCAdOgAwIBAgIKPPYHqWhwDtqLhDAKBggqhkjOPQQDAzBrMQswCQYDVQQG /etc/ssl/certs/ca-certificates.crt

WORKDIR /app

# Copy deployed app with all dependencies
COPY --from=builder /deploy ./

# Expose port 3000
EXPOSE 3000

# Set production environment
ENV NODE_ENV=production

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:3000/health || exit 1

# Start the Remix server (use node_modules binary directly to avoid npx issues)
CMD ["node", "./node_modules/@remix-run/serve/dist/cli.js", "./build/server/index.js"]
