#!/usr/bin/env bash
#
# Deploy Netclode to a server
#
# Usage: ./scripts/deploy.sh [hostname]
#
set -euo pipefail

HOST="${1:-netclode}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== Deploying Netclode to $HOST ==="

# Check if we can reach the host
if ! ssh -o ConnectTimeout=5 "root@$HOST" true 2>/dev/null; then
    echo "Error: Cannot connect to root@$HOST"
    echo "Make sure the host is reachable and SSH is configured"
    exit 1
fi

# Deploy NixOS configuration (if on NixOS)
if ssh "root@$HOST" 'test -f /etc/NIXOS' 2>/dev/null; then
    echo ""
    echo "=== Deploying NixOS configuration ==="
    cd "$PROJECT_DIR/infra/nixos"
    nixos-rebuild switch --flake ".#netclode" --target-host "root@$HOST" --use-remote-sudo
    cd "$PROJECT_DIR"
fi

# Sync application code
echo ""
echo "=== Syncing application code ==="
rsync -avz --delete \
    --exclude 'node_modules' \
    --exclude '.git' \
    --exclude '.turbo' \
    --exclude 'dist' \
    --exclude '.env' \
    "$PROJECT_DIR/apps/" "root@$HOST:/opt/netclode/apps/"

rsync -avz --delete \
    --exclude 'node_modules' \
    --exclude 'dist' \
    "$PROJECT_DIR/packages/" "root@$HOST:/opt/netclode/packages/"

rsync -avz \
    "$PROJECT_DIR/package.json" \
    "$PROJECT_DIR/bun.lock" \
    "$PROJECT_DIR/tsconfig.json" \
    "$PROJECT_DIR/tsconfig.base.json" \
    "root@$HOST:/opt/netclode/"

# Install dependencies and restart
echo ""
echo "=== Installing dependencies and restarting ==="
ssh "root@$HOST" << 'EOF'
    cd /opt/netclode
    bun install --frozen-lockfile
    systemctl restart netclode
    echo "Service restarted. Checking status..."
    sleep 2
    systemctl status netclode --no-pager || true
EOF

echo ""
echo "=== Deployment complete ==="
echo "Control plane: https://$HOST (via Tailscale)"
