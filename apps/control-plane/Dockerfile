# Control Plane Dockerfile
FROM oven/bun:1.3-alpine AS builder

WORKDIR /app

# Copy workspace files
COPY package.json bun.lock ./
COPY packages/protocol ./packages/protocol
COPY apps/control-plane ./apps/control-plane

# Install dependencies
RUN bun install --frozen-lockfile

# Build
WORKDIR /app/apps/control-plane
RUN bun run build

# Production image
FROM oven/bun:1.3-alpine

WORKDIR /app

COPY --from=builder /app/apps/control-plane/dist ./dist
COPY --from=builder /app/apps/control-plane/package.json ./
COPY --from=builder /app/node_modules ./node_modules

ENV NODE_ENV=production
ENV PORT=3000

EXPOSE 3000

CMD ["bun", "run", "dist/index.js"]
