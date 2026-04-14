# Netclode Agent — Boxlite-compatible image
#
# This is a simplified agent image for Boxlite deployments.
# Boxlite handles: VM isolation, secret injection (MITM), network allowlists.
# So this image does NOT need: auth-proxy, Docker-in-Docker, iptables, CA certs.
#
# Multi-arch: builds for both amd64 and arm64.
#
# Layer ordering optimized for cache efficiency:
#   1. Base OS packages (rarely changes)
#   2. Global tool installs (changes on tool version bumps)
#   3. Package dependency install (changes on package.json/lock updates)
#   4. Source copy + bundle (changes on every code change)

FROM debian:trixie-slim AS base

ARG TARGETARCH

# Install essentials + mise (no docker, no iptables — Boxlite handles isolation)
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    curl \
    git \
    ca-certificates \
    build-essential \
    sudo \
    && rm -rf /var/lib/apt/lists/* \
    && curl https://mise.run | sh

ENV PATH="/root/.local/bin:/root/.local/share/mise/shims:${PATH}"

# =============================================================================
# Builder
# =============================================================================
FROM base AS builder

ARG TARGETARCH

# Install Python for node-gyp (required by node-pty) and Node via mise
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/* && \
    mise install node@24 && \
    mise use -g node@24 && \
    mise reshim

WORKDIR /build

# Install esbuild for bundling
RUN npm install -g esbuild

# =============================================================================
# Global tool installs (cached layer — expensive, rarely changes)
# =============================================================================

# Claude CLI
RUN --mount=type=cache,target=/root/.npm \
    npm install -g @anthropic-ai/claude-code

# Copilot CLI
RUN --mount=type=cache,target=/root/.npm \
    npm install -g @github/copilot

# OpenCode CLI (multi-arch)
RUN ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "x64") && \
    curl -fsSL "https://github.com/anomalyco/opencode/releases/latest/download/opencode-linux-${ARCH}.tar.gz" -o /tmp/opencode.tar.gz && \
    tar -xzf /tmp/opencode.tar.gz -C /tmp && \
    mv /tmp/opencode /usr/local/bin/opencode && \
    chmod +x /usr/local/bin/opencode && \
    rm -rf /tmp/opencode.tar.gz

# GitHub CLI (multi-arch)
RUN ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "arm64" || echo "amd64") && \
    GH_VERSION=$(curl -fsSL https://api.github.com/repos/cli/cli/releases/latest | grep -oP '"tag_name": "v\K[^"]+') && \
    curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_${ARCH}.tar.gz" -o /tmp/gh.tar.gz && \
    tar -xzf /tmp/gh.tar.gz -C /tmp && \
    mv /tmp/gh_*/bin/gh /usr/local/bin/gh && \
    chmod +x /usr/local/bin/gh && \
    rm -rf /tmp/gh.tar.gz /tmp/gh_*

# Remove C headers from Node (only needed for compiling native modules)
# Remove unused platform binaries from global @github/copilot CLI
RUN KEEP_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "linux-arm64" || echo "linux-x64") && \
    rm -rf /root/.local/share/mise/installs/node/24/include && \
    find /root/.local/share/mise/installs/node/24/lib/node_modules/@github/copilot/prebuilds/ \
      -mindepth 1 -maxdepth 1 ! -name "$KEEP_ARCH" -exec rm -rf {} + 2>/dev/null || true && \
    find /root/.local/share/mise/installs/node/24/lib/node_modules/@github/copilot/ripgrep/bin/ \
      -mindepth 1 -maxdepth 1 ! -name "$KEEP_ARCH" -exec rm -rf {} + 2>/dev/null || true

# =============================================================================
# Agent dependency install (cached unless package.json/lock change)
# =============================================================================
COPY services/agent/package.json services/agent/package-lock.json /build/services/agent/
WORKDIR /build/services/agent
RUN --mount=type=cache,target=/root/.npm npm install

# =============================================================================
# Agent source copy + bundle
# =============================================================================
COPY services/agent /build/services/agent

# Bundle agent
RUN esbuild src/index.ts --bundle --platform=node --format=esm --packages=external --outfile=dist/agent.js

# Remove devDependencies
RUN npm prune --omit=dev

# Remove unused platform binaries from node_modules
RUN KEEP_VENDOR=$([ "$TARGETARCH" = "arm64" ] && echo "aarch64-unknown-linux" || echo "x86_64-unknown-linux") && \
    find node_modules/@openai/codex-sdk/vendor/ -mindepth 1 -maxdepth 1 \
      ! -name "${KEEP_VENDOR}*" -exec rm -rf {} + 2>/dev/null || true && \
    KEEP_PREBUILT=$([ "$TARGETARCH" = "arm64" ] && echo "linux-arm64" || echo "linux-x64") && \
    find node_modules/@github/copilot/prebuilds/ -mindepth 1 -maxdepth 1 \
      ! -name "$KEEP_PREBUILT" -exec rm -rf {} + 2>/dev/null || true && \
    find node_modules/@github/copilot/ripgrep/bin/ -mindepth 1 -maxdepth 1 \
      ! -name "$KEEP_PREBUILT" -exec rm -rf {} + 2>/dev/null || true

# =============================================================================
# Runtime
# =============================================================================
FROM base AS runtime

ARG TARGETARCH

# Create non-root user with passwordless sudo
# Note: /agent is the persistent home dir (Boxlite virtiofs mount)
# No docker group needed — Boxlite handles isolation externally
RUN useradd -m -u 1000 -d /agent agent && \
    echo 'agent ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/agent && \
    chmod 0440 /etc/sudoers.d/agent && \
    mkdir -p /agent /opt/agent && \
    chown -R agent:agent /agent /opt/agent

# Install bun (multi-arch)
RUN apt-get update && apt-get install -y --no-install-recommends unzip && \
    BUN_ARCH=$([ "$TARGETARCH" = "arm64" ] && echo "aarch64" || echo "x64") && \
    curl -fsSL "https://github.com/oven-sh/bun/releases/latest/download/bun-linux-${BUN_ARCH}.zip" -o /tmp/bun.zip && \
    unzip /tmp/bun.zip -d /tmp && \
    mv /tmp/bun-linux-*/bun /opt/bun && \
    chmod +x /opt/bun && \
    rm -rf /tmp/bun.zip /tmp/bun-linux-* /var/lib/apt/lists/*

# Copy stable binaries
COPY --from=builder /usr/local/bin/opencode /usr/local/bin/opencode
COPY --from=builder /usr/local/bin/gh /usr/local/bin/gh

# Copy mise binary + Node
RUN mkdir -p /opt/mise/bin && \
    cp /root/.local/bin/mise /opt/mise/bin/
COPY --from=builder /root/.local/share/mise/installs/node/24 /opt/node

# Pre-install OpenCode plugin
RUN OPENCODE_VERSION=$(/usr/local/bin/opencode --version) && \
    export HOME=/opt/bun-cache-home && \
    mkdir -p $HOME/.bun && \
    mkdir -p /opt/opencode-config && cd /opt/opencode-config && \
    echo '{"name": "opencode-config", "private": true}' > package.json && \
    if /opt/bun add "@opencode-ai/plugin@${OPENCODE_VERSION}" "@opencode-ai/sdk@${OPENCODE_VERSION}" "zod" --exact; then \
      echo "Preinstalled OpenCode plugin ${OPENCODE_VERSION}"; \
    else \
      echo "Warning: OpenCode plugin ${OPENCODE_VERSION} not found, falling back to 1.1.44"; \
      /opt/bun add "@opencode-ai/plugin@1.1.44" "@opencode-ai/sdk@1.1.44" "zod@4.1.8" --exact; \
    fi && \
    if [ -d "$HOME/.bun" ]; then mv $HOME/.bun /opt/bun-cache; fi && \
    rm -rf $HOME

# Shell profile for agent user
RUN echo 'export MISE_DATA_DIR=/agent/.local/share/mise' >> /etc/profile.d/mise.sh && \
    echo 'export MISE_CACHE_DIR=/agent/.cache/mise' >> /etc/profile.d/mise.sh && \
    echo 'export XDG_CONFIG_HOME=/agent/.local/config' >> /etc/profile.d/mise.sh && \
    echo 'export PATH="/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:/opt:$PATH"' >> /etc/profile.d/mise.sh && \
    chmod +x /etc/profile.d/mise.sh && \
    echo 'export MISE_DATA_DIR=/agent/.local/share/mise' >> /etc/bash.bashrc && \
    echo 'export MISE_CACHE_DIR=/agent/.cache/mise' >> /etc/bash.bashrc && \
    echo 'export XDG_CONFIG_HOME=/agent/.local/config' >> /etc/bash.bashrc && \
    echo 'export PATH="/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:/opt:$PATH"' >> /etc/bash.bashrc && \
    echo 'export PS1='"'"'\[\033[01;32m\]\u\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '"'" >> /etc/bash.bashrc

# Copy agent bundle + node_modules (changes most often — last for cache)
COPY --from=builder --chown=agent:agent /build/services/agent/dist/agent.js /opt/agent/
COPY --from=builder --chown=agent:agent /build/services/agent/node_modules /opt/agent/node_modules

# Copy entrypoint script
COPY services/agent/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /opt/agent

ENV NODE_ENV=production
ENV HOME=/agent
ENV MISE_DATA_DIR=/agent/.local/share/mise
ENV MISE_CACHE_DIR=/agent/.cache/mise
ENV XDG_CONFIG_HOME=/agent/.local/config
ENV PATH="/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:/opt:${PATH}"

EXPOSE 3002

ENTRYPOINT ["/entrypoint.sh"]
