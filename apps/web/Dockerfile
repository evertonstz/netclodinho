# Web App Dockerfile
FROM oven/bun:1.3-alpine AS builder

WORKDIR /app

# Copy workspace files
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/
COPY packages/protocol/package.json packages/protocol/

# Install dependencies
RUN bun install --frozen-lockfile

# Copy source code
COPY apps/web apps/web
COPY packages/protocol packages/protocol

# Build
WORKDIR /app/apps/web
RUN bun run build

# Production image with nginx
FROM nginx:alpine

# Copy built static files
COPY --from=builder /app/apps/web/dist /usr/share/nginx/html

# Copy nginx configuration
COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
