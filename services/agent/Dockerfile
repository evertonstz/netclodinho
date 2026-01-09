# Debian-slim + mise agent image
#
# On-demand tooling via:
#   mise install python@3.12
#   mise install go@latest
#   mise install rust@latest
#
FROM debian:trixie-slim AS base

# Install essentials + mise + docker
# Note: Use iptables-legacy because Kata VM kernel lacks nftables support
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    curl \
    git \
    ca-certificates \
    build-essential \
    docker.io \
    docker-cli \
    iptables \
    && update-alternatives --set iptables /usr/sbin/iptables-legacy \
    && update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy \
    && rm -rf /var/lib/apt/lists/* \
    && curl https://mise.run | sh

ENV PATH="/root/.local/bin:/root/.local/share/mise/shims:${PATH}"

# Install node via mise
RUN mise install node@24 && \
    mise use -g node@24 && \
    mise reshim

# =============================================================================
# Builder
# =============================================================================
FROM base AS builder

# Install Python for node-gyp (required by node-pty)
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Install esbuild for bundling
RUN npm install -g esbuild

# Copy source files
COPY packages/protocol packages/protocol
COPY services/agent services/agent

# Install agent dependencies
# Remove workspace protocol reference (it's bundled by esbuild) and install
WORKDIR /build/services/agent
RUN --mount=type=cache,target=/root/.npm \
    cat package.json | sed '/"@netclode\/protocol"/d' > package.tmp.json && \
    mv package.tmp.json package.json && \
    npm install

# Bundle agent (inline local dependencies)
RUN esbuild src/index.ts --bundle --platform=node --format=esm --packages=external --outfile=dist/agent.js

# Claude CLI
RUN --mount=type=cache,target=/root/.npm \
    npm install -g @anthropic-ai/claude-code

# =============================================================================
# Runtime
# =============================================================================
FROM base AS runtime

# Create non-root user with docker access
# Note: /agent is the persistent home dir, mounted from JuiceFS
RUN useradd -m -u 1000 -G docker -d /agent agent && \
    mkdir -p /agent /opt/agent && \
    chown -R agent:agent /agent /opt/agent

# Copy bundled app and dependencies
COPY --from=builder --chown=agent:agent /build/services/agent/dist/agent.js /opt/agent/
COPY --from=builder --chown=agent:agent /build/services/agent/node_modules /opt/agent/node_modules

# Copy mise and node to /opt (NOT /agent which is a volume mount)
# mise binary and shims need to be in a persistent location
RUN mkdir -p /opt/mise/bin /opt/mise/shims && \
    cp /root/.local/bin/mise /opt/mise/bin/ && \
    cp -r /root/.local/share/mise /opt/mise/share
COPY --from=builder /root/.local/share/mise/installs/node/24 /opt/node

# Create profile for agent user that sets PATH and mise env vars
# This ensures any shell spawned (including by Claude Code) has access to mise
# Add to both /etc/profile.d (login shells) and /etc/bash.bashrc (interactive non-login)
# MISE_DATA_DIR is in /agent so installed tools persist across pod restarts
# Shims path is included so mise-installed tools are available
RUN echo 'export MISE_DATA_DIR=/agent/.local/share/mise' >> /etc/profile.d/mise.sh && \
    echo 'export MISE_CACHE_DIR=/agent/.cache/mise' >> /etc/profile.d/mise.sh && \
    echo 'export PATH="/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:$PATH"' >> /etc/profile.d/mise.sh && \
    chmod +x /etc/profile.d/mise.sh && \
    echo 'export MISE_DATA_DIR=/agent/.local/share/mise' >> /etc/bash.bashrc && \
    echo 'export MISE_CACHE_DIR=/agent/.cache/mise' >> /etc/bash.bashrc && \
    echo 'export PATH="/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:$PATH"' >> /etc/bash.bashrc

# Copy entrypoint script
COPY services/agent/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

WORKDIR /opt/agent

ENV NODE_ENV=production
ENV HOME=/agent
ENV MISE_DATA_DIR=/agent/.local/share/mise
ENV MISE_CACHE_DIR=/agent/.cache/mise
ENV PATH="/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:${PATH}"

EXPOSE 3002

# Start as root to run dockerd, then drop to agent user
ENTRYPOINT ["/entrypoint.sh"]
