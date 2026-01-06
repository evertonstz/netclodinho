# Minimal Nix agent image (using flakes)
#
# Agent installs tools on-demand via:
#   nix profile install nixpkgs#python3
#   nix profile install nixpkgs#go
#   nix shell nixpkgs#rustc nixpkgs#cargo
#
FROM nixos/nix:latest AS base

# Configure Nix for flakes (no channels needed)
RUN echo "experimental-features = nix-command flakes" >> /etc/nix/nix.conf && \
    echo "accept-flake-config = true" >> /etc/nix/nix.conf

# Install bun and nodejs via flakes (claude CLI needs node)
RUN nix profile install nixpkgs#bun nixpkgs#nodejs && \
    nix-collect-garbage -d

# =============================================================================
# Builder stage
# =============================================================================
FROM base AS builder

WORKDIR /tmp/app

COPY package.json bun.lock ./
COPY packages/protocol/package.json packages/protocol/
COPY apps/agent/package.json apps/agent/

RUN bun install

# Install Claude Code CLI globally
RUN bun add -g @anthropic-ai/claude-code

COPY packages/protocol packages/protocol
COPY apps/agent apps/agent

# =============================================================================
# Runtime stage
# =============================================================================
FROM base AS runtime

# Create non-root user for running claude with --dangerously-skip-permissions
RUN echo "agent:x:1000:1000::/home/agent:/bin/sh" >> /etc/passwd && \
    echo "agent:x:1000:" >> /etc/group && \
    mkdir -p /home/agent && chown 1000:1000 /home/agent

# Create workspace and app directories
RUN mkdir -p /workspace /opt/agent && chown -R agent:agent /workspace /opt/agent

# Copy app
COPY --from=builder /tmp/app/node_modules /opt/agent/node_modules
COPY --from=builder /tmp/app/apps/agent /opt/agent/apps/agent
COPY --from=builder /tmp/app/packages /opt/agent/packages
COPY --from=builder /root/.bun /home/agent/.bun
RUN chown -R agent:agent /home/agent/.bun /opt/agent

# Switch to non-root user
USER agent

WORKDIR /opt/agent/apps/agent

ENV NODE_ENV=production
ENV WORKSPACE=/workspace
ENV PATH="/home/agent/.bun/bin:/nix/var/nix/profiles/default/bin:${PATH}"
ENV HOME=/home/agent

EXPOSE 3002

CMD ["bun", "run", "src/index.ts"]
