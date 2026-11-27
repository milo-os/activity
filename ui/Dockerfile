# Build stage
FROM node:26.13.1-alpine3.24 AS builder

WORKDIR /app

# Copy workspace and lock files first for better caching
COPY pnpm-workspace.yaml ./
COPY pnpm-lock.yaml ./
COPY package.json ./

# Node 26 ships no corepack, so install the pnpm that package.json's
# packageManager pins; it stays in this stage only.
RUN npm install -g "$(node -p 'require("./package.json").packageManager.split("+")[0]')" --no-audit --no-fund

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
COPY example/react-router.config.ts ./example/
COPY example/tailwind.config.js ./example/
COPY example/postcss.config.js ./example/

# Install dependencies and build. Deploy from the lockfile, not with --legacy:
# under pnpm 12 the legacy deploy drops @datum-cloud/datum-ui's optional peers
# (date-fns-tz, recharts and others), and the server fails to start.
RUN pnpm install --frozen-lockfile --prod=false --ignore-scripts && \
    pnpm --filter @datum-cloud/activity-ui build && \
    pnpm --filter activity-ui-example build && \
    pnpm deploy --filter activity-ui-example --prod --ignore-scripts /deploy && \
    cp -r example/build /deploy/build

# Runtime stage - Node.js for React Router server
FROM node:26.13.1-alpine3.24 AS runtime

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

# Start the React Router server (use node_modules binary directly to avoid npx issues)
CMD ["node", "./node_modules/@react-router/serve/bin.js", "./build/server/index.js"]
