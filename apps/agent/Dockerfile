# Debian-slim + mise agent image
#
# On-demand tooling via:
#   mise install python@3.12
#   mise install go@latest
#   mise install rust@latest
#
FROM debian:bookworm-slim AS base

# Install essentials + mise + docker
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    curl \
    git \
    ca-certificates \
    build-essential \
    docker.io \
    iptables \
    && rm -rf /var/lib/apt/lists/* \
    && curl https://mise.run | sh

ENV PATH="/root/.local/bin:/root/.local/share/mise/shims:${PATH}"

# Install node via mise
RUN mise install node@22 && \
    mise use -g node@22 && \
    mise reshim

# =============================================================================
# Builder
# =============================================================================
FROM base AS builder

WORKDIR /build

# Install esbuild for bundling
RUN npm install -g esbuild

# Copy source files
COPY packages/protocol packages/protocol
COPY apps/agent apps/agent

# Install agent dependencies (fresh package.json without workspace protocol)
WORKDIR /build/apps/agent
RUN echo '{"type":"module"}' > package.json && \
    npm install @anthropic-ai/claude-agent-sdk @anthropic-ai/sdk

# Bundle agent (inline local dependencies)
RUN esbuild src/index.ts --bundle --platform=node --format=esm --packages=external --outfile=dist/agent.js

# Claude CLI
RUN npm install -g @anthropic-ai/claude-code

# =============================================================================
# Runtime
# =============================================================================
FROM base AS runtime

# Create non-root user with docker access
RUN useradd -m -u 1000 -G docker agent && \
    mkdir -p /workspace /opt/agent && \
    chown -R agent:agent /workspace /opt/agent

# Copy mise for agent user (including config)
RUN mkdir -p /home/agent/.local /home/agent/.config && \
    cp -r /root/.local/bin /home/agent/.local/ && \
    cp -r /root/.local/share /home/agent/.local/ && \
    cp -r /root/.config/mise /home/agent/.config/ 2>/dev/null || true && \
    chown -R agent:agent /home/agent/.local /home/agent/.config

# Copy bundled app and dependencies
COPY --from=builder --chown=agent:agent /build/apps/agent/dist/agent.js /opt/agent/
COPY --from=builder --chown=agent:agent /build/apps/agent/node_modules /opt/agent/node_modules

# Copy global npm modules (claude CLI)
COPY --from=builder --chown=agent:agent /root/.local/share/mise/installs/node/22/lib/node_modules /home/agent/.local/share/mise/installs/node/22/lib/node_modules
COPY --from=builder --chown=agent:agent /root/.local/share/mise/installs/node/22/bin /home/agent/.local/share/mise/installs/node/22/bin

# Copy entrypoint script
COPY apps/agent/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /opt/agent

ENV NODE_ENV=production
ENV WORKSPACE=/workspace
ENV HOME=/workspace

# Persist all tool data and caches on /workspace (JuiceFS)
ENV MISE_DATA_DIR=/workspace/.mise
ENV MISE_CACHE_DIR=/workspace/.cache/mise
ENV GOPATH=/workspace/.go
ENV GOMODCACHE=/workspace/.go/pkg/mod
ENV CARGO_HOME=/workspace/.cargo
ENV RUSTUP_HOME=/workspace/.rustup
ENV PIP_CACHE_DIR=/workspace/.cache/pip
ENV COMPOSER_HOME=/workspace/.composer
ENV PNPM_HOME=/workspace/.pnpm
ENV NPM_CONFIG_CACHE=/workspace/.npm
ENV YARN_CACHE_FOLDER=/workspace/.cache/yarn

ENV PATH="/workspace/.mise/shims:/workspace/.go/bin:/workspace/.cargo/bin:/workspace/.pnpm:/home/agent/.local/share/mise/installs/node/22/bin:/home/agent/.local/bin:/home/agent/.local/share/mise/shims:${PATH}"

EXPOSE 3002

# Start as root to run dockerd, then drop to agent user
ENTRYPOINT ["/entrypoint.sh"]
