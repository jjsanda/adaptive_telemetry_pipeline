# syntax=docker/dockerfile:1

# --- Build stage: install all deps, compile TS, prune to production deps ---
FROM node:22-slim AS build
WORKDIR /app

# Install dependencies against the lockfile for reproducible builds.
COPY package.json package-lock.json ./
RUN npm ci

# Compile TypeScript -> dist/.
COPY tsconfig.json ./
COPY src ./src
RUN npm run build

# Drop devDependencies so only runtime deps are carried into the final image.
RUN npm prune --omit=dev

# --- Runtime stage: slim, non-root ---
FROM node:22-slim AS runtime
ENV NODE_ENV=production
WORKDIR /app

COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY --from=build /app/package.json ./package.json

# `node` is a pre-existing non-root user in the official image.
USER node

EXPOSE 8082

# Instrumentation is preloaded via --import so the OTel SDK starts before the
# HTTP framework is imported.
CMD ["node", "--import", "./dist/instrumentation.js", "dist/index.js"]
