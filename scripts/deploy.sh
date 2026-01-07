#!/usr/bin/env bash
#
# Deploy Netclode: wait for CI then rollout
#
# Usage: ./scripts/deploy.sh [deployment...]
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== Waiting for CI to complete ==="
RUN_ID=$(gh run list --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$RUN_ID" --exit-status

echo ""
exec "$SCRIPT_DIR/rollout.sh" "$@"
