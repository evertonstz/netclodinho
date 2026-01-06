#!/usr/bin/env bash
#
# Rollout Kubernetes deployments after CI builds new images
#
# Usage: ./scripts/rollout.sh [deployment...]
#   If no deployments specified, rolls out control-plane and web
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Load .env if exists
if [[ -f "$PROJECT_DIR/.env" ]]; then
  export $(grep -v '^#' "$PROJECT_DIR/.env" | xargs)
fi

if [[ -z "${DEPLOY_HOST:-}" ]]; then
  echo "Error: DEPLOY_HOST not set. Set it in .env or environment."
  exit 1
fi
HOST="$DEPLOY_HOST"
NAMESPACE="netclode"
DEPLOYMENTS=("${@:-control-plane web}")

echo "=== Rolling out to $HOST ==="

for deployment in "${DEPLOYMENTS[@]}"; do
  echo ""
  echo "=== Rolling out $deployment ==="
  ssh "$HOST" "KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl -n $NAMESPACE rollout restart deployment/$deployment"
  ssh "$HOST" "KUBECONFIG=/etc/rancher/k3s/k3s.yaml kubectl -n $NAMESPACE rollout status deployment/$deployment"
done

echo ""
echo "=== Rollout complete ==="
