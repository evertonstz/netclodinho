# Optimized Nix agent image
#
# On-demand tooling via:
#   nix profile install nixpkgs#python3
#   nix shell nixpkgs#rustc nixpkgs#cargo
#
FROM nixos/nix:latest AS base

# Configure nix, create user, set up all directories BEFORE chown
RUN echo "experimental-features = nix-command flakes" >> /etc/nix/nix.conf && \
    echo "accept-flake-config = true" >> /etc/nix/nix.conf && \
    echo "agent:x:1000:1000::/home/agent:/bin/sh" >> /etc/passwd && \
    echo "agent:x:1000:" >> /etc/group && \
    mkdir -p /home/agent /workspace /opt/agent && \
    chown -R 1000:1000 /home/agent /workspace /opt/agent /nix

# Install as agent user
USER agent
ENV HOME=/home/agent
RUN nix profile install nixpkgs#nodejs nixpkgs#bashInteractive nixpkgs#coreutils nixpkgs#nix nixpkgs#git nixpkgs#cacert && \
    nix-collect-garbage -d

# =============================================================================
# Builder
# =============================================================================
FROM base AS builder

ENV PATH="/home/agent/.nix-profile/bin:/nix/var/nix/profiles/default/bin:${PATH}"
WORKDIR /build

# Copy source files
COPY --chown=agent packages/protocol packages/protocol
COPY --chown=agent apps/agent apps/agent

# Install dependencies and build
WORKDIR /build/apps/agent
RUN echo '{"type":"module"}' > package.json && \
    npm install @anthropic-ai/claude-agent-sdk @anthropic-ai/sdk esbuild && \
    ./node_modules/.bin/esbuild src/index.ts --bundle --platform=node --format=esm --packages=external --outfile=dist/agent.js

# Claude CLI
RUN npm install -g @anthropic-ai/claude-code

# =============================================================================
# Runtime
# =============================================================================
FROM base AS runtime

# Copy bundled app and dependencies
COPY --from=builder --chown=agent:agent /build/apps/agent/dist/agent.js /opt/agent/
COPY --from=builder --chown=agent:agent /build/apps/agent/node_modules /opt/agent/node_modules

# Claude CLI
COPY --from=builder --chown=agent:agent /home/agent/.nix-profile/lib/node_modules /home/agent/.nix-profile/lib/node_modules
COPY --from=builder --chown=agent:agent /home/agent/.nix-profile/bin /home/agent/.nix-profile/bin

WORKDIR /opt/agent

ENV NODE_ENV=production
ENV WORKSPACE=/workspace
ENV PATH="/home/agent/.nix-profile/bin:/nix/var/nix/profiles/default/bin:${PATH}"
ENV HOME=/home/agent
ENV NIX_CONF_DIR=/etc/nix
ENV NIX_SSL_CERT_FILE=/home/agent/.nix-profile/etc/ssl/certs/ca-bundle.crt
ENV SSL_CERT_FILE=/home/agent/.nix-profile/etc/ssl/certs/ca-bundle.crt

EXPOSE 3002

CMD ["node", "agent.js"]
