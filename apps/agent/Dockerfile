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
# Note: /agent is the persistent home dir, mounted from JuiceFS
RUN useradd -m -u 1000 -G docker -d /agent agent && \
    mkdir -p /agent /opt/agent && \
    chown -R agent:agent /agent /opt/agent

# Copy mise for agent user (including config)
RUN mkdir -p /agent/.local /agent/.config && \
    cp -r /root/.local/bin /agent/.local/ && \
    cp -r /root/.local/share /agent/.local/ && \
    cp -r /root/.config/mise /agent/.config/ 2>/dev/null || true && \
    chown -R agent:agent /agent/.local /agent/.config

# Copy bundled app and dependencies
COPY --from=builder --chown=agent:agent /build/apps/agent/dist/agent.js /opt/agent/
COPY --from=builder --chown=agent:agent /build/apps/agent/node_modules /opt/agent/node_modules

# Copy global npm modules (claude CLI)
COPY --from=builder --chown=agent:agent /root/.local/share/mise/installs/node/22/lib/node_modules /agent/.local/share/mise/installs/node/22/lib/node_modules
COPY --from=builder --chown=agent:agent /root/.local/share/mise/installs/node/22/bin /agent/.local/share/mise/installs/node/22/bin

# Copy entrypoint script
COPY apps/agent/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /opt/agent

ENV NODE_ENV=production
ENV HOME=/agent
ENV PATH="/agent/.local/share/mise/shims:/agent/.local/bin:/agent/.local/share/mise/installs/node/22/bin:${PATH}"

EXPOSE 3002

# Start as root to run dockerd, then drop to agent user
ENTRYPOINT ["/entrypoint.sh"]
