# Alpine + Nix agent image for Kata Containers
#
# Uses Alpine as base (works with read-only rootfs) and Nix for packages.
#
FROM alpine:3.21 AS base

# Install base dependencies
# Note: coreutils required for Nix installer (BusyBox cp is incompatible)
RUN apk add --no-cache \
    bash \
    curl \
    git \
    openssh-client \
    ca-certificates \
    xz \
    coreutils \
    findutils \
    gawk \
    gnupg

# Create /nix directory (required before Nix install)
RUN mkdir -m 0755 /nix && chown root /nix

# Install Nix (single-user mode for containers)
RUN curl -L https://nixos.org/nix/install | sh -s -- --no-daemon --no-channel-add

# Set up Nix environment
ENV PATH="/root/.nix-profile/bin:/nix/var/nix/profiles/default/bin:${PATH}"

# Enable flakes and configure Nix
RUN mkdir -p /root/.config/nix && \
    echo "experimental-features = nix-command flakes" > /root/.config/nix/nix.conf

# Install essential packages via Nix
RUN . /root/.nix-profile/etc/profile.d/nix.sh && \
    nix-channel --add https://nixos.org/channels/nixos-unstable nixpkgs && \
    nix-channel --update && \
    nix-env -iA \
      nixpkgs.bun \
      nixpkgs.nodejs_22 \
      nixpkgs.git \
      nixpkgs.gh \
      nixpkgs.curl \
      nixpkgs.jq \
      nixpkgs.ripgrep \
      nixpkgs.fd \
      nixpkgs.tree \
      nixpkgs.gnumake \
      nixpkgs.gcc

# Build stage - install dependencies
FROM base AS builder

WORKDIR /app

# Copy workspace config and lockfile
COPY package.json bun.lock ./
COPY packages/protocol/package.json packages/protocol/
COPY apps/agent/package.json apps/agent/

# Install dependencies
RUN . /root/.nix-profile/etc/profile.d/nix.sh && bun install --frozen-lockfile

# Copy source code
COPY packages/protocol packages/protocol
COPY apps/agent apps/agent

# Build (if needed)
WORKDIR /app/apps/agent

# Runtime stage
FROM base AS runtime

# Create directories
RUN mkdir -p /workspace /opt/agent /home/agent && \
    adduser -D -h /home/agent -s /bin/bash agent && \
    chown -R agent:agent /workspace /home/agent

# Copy built app and dependencies
COPY --from=builder /app/node_modules /opt/agent/node_modules
COPY --from=builder /app/apps/agent /opt/agent/apps/agent
COPY --from=builder /app/packages /opt/agent/packages

WORKDIR /opt/agent/apps/agent

# Environment
ENV NODE_ENV=production
ENV WORKSPACE=/workspace
ENV HOME=/home/agent
ENV PATH="/root/.nix-profile/bin:/nix/var/nix/profiles/default/bin:${PATH}"

EXPOSE 3002

# Run agent
CMD ["/root/.nix-profile/bin/bun", "run", "src/index.ts"]
