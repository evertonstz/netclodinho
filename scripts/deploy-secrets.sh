#!/usr/bin/env bash
# Deploy secrets from .env to Kubernetes and NixOS host
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
ENV_FILE="${PROJECT_ROOT}/.env"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() {
	echo -e "${RED}[x]${NC} $1"
	exit 1
}

# Check .env exists
if [[ ! -f "$ENV_FILE" ]]; then
	error ".env file not found at $ENV_FILE"
fi

# Load .env
set -a
source "$ENV_FILE"
set +a

# Validate required variables
required_vars=(
	"ANTHROPIC_API_KEY"
	"DO_SPACES_ACCESS_KEY"
	"DO_SPACES_SECRET_KEY"
	"TS_OAUTH_CLIENT_ID"
	"TS_OAUTH_CLIENT_SECRET"
)

for var in "${required_vars[@]}"; do
	if [[ -z "${!var:-}" ]]; then
		error "Missing required variable: $var"
	fi
done

# Optional: Host Tailscale auth key (for SSH access)
if [[ -z "${TAILSCALE_AUTHKEY:-}" ]]; then
	warn "TAILSCALE_AUTHKEY not set - host won't join Tailscale"
	warn "You can still SSH via regular IP, or set it for Tailscale SSH"
fi

# JuiceFS configuration
JUICEFS_BUCKET="${JUICEFS_BUCKET:-https://nyc3.digitaloceanspaces.com/netclode}"
# Use cluster-internal Redis service for JuiceFS metadata
JUICEFS_META_URL="${JUICEFS_META_URL:-redis://redis-juicefs.netclode.svc.cluster.local:6379/0}"

echo ""
log "Deploying secrets..."
echo ""

# Check if we're deploying to remote or local
if [[ "${DEPLOY_HOST:-}" ]]; then
	log "Deploying to remote host: $DEPLOY_HOST"
	SSH_CMD="ssh $DEPLOY_HOST"
	KUBECTL_CMD="$SSH_CMD kubectl"
else
	log "Deploying locally"
	SSH_CMD=""
	KUBECTL_CMD="kubectl"
fi

# Create namespace if not exists
$KUBECTL_CMD create namespace netclode --dry-run=client -o yaml | $KUBECTL_CMD apply -f -

# Deploy Kubernetes secrets
log "Creating Kubernetes secrets..."

# Netclode secrets (control plane)
# Build optional args for secrets
OPTIONAL_ARGS=""
if [[ -n "${GITHUB_COPILOT_TOKEN:-}" ]]; then
	OPTIONAL_ARGS="$OPTIONAL_ARGS --from-literal=github-copilot-token=$GITHUB_COPILOT_TOKEN"
fi
if [[ -n "${GITHUB_APP_ID:-}" ]]; then
	OPTIONAL_ARGS="$OPTIONAL_ARGS --from-literal=github-app-id=$GITHUB_APP_ID"
fi
if [[ -n "${GITHUB_APP_PRIVATE_KEY_B64:-}" ]]; then
	OPTIONAL_ARGS="$OPTIONAL_ARGS --from-literal=github-app-private-key=$(echo $GITHUB_APP_PRIVATE_KEY_B64 | base64 -d)"
fi
if [[ -n "${GITHUB_INSTALLATION_ID:-}" ]]; then
	OPTIONAL_ARGS="$OPTIONAL_ARGS --from-literal=github-installation-id=$GITHUB_INSTALLATION_ID"
fi
$KUBECTL_CMD -n netclode create secret generic netclode-secrets \
	--from-literal=anthropic-api-key="$ANTHROPIC_API_KEY" \
	$OPTIONAL_ARGS \
	--dry-run=client -o yaml | $KUBECTL_CMD apply -f -

# JuiceFS secrets (for CSI driver)
$KUBECTL_CMD -n netclode create secret generic juicefs-secret \
	--from-literal=name=netclode \
	--from-literal=metaurl="$JUICEFS_META_URL" \
	--from-literal=storage=s3 \
	--from-literal=bucket="$JUICEFS_BUCKET" \
	--from-literal=access-key="$DO_SPACES_ACCESS_KEY" \
	--from-literal=secret-key="$DO_SPACES_SECRET_KEY" \
	--dry-run=client -o yaml | $KUBECTL_CMD apply -f -

# Tailscale operator secrets (if OAuth credentials provided)
if [[ -n "${TS_OAUTH_CLIENT_ID:-}" ]] && [[ -n "${TS_OAUTH_CLIENT_SECRET:-}" ]]; then
	$KUBECTL_CMD create namespace tailscale --dry-run=client -o yaml | $KUBECTL_CMD apply -f -
	$KUBECTL_CMD -n tailscale create secret generic operator-oauth \
		--from-literal=client_id="$TS_OAUTH_CLIENT_ID" \
		--from-literal=client_secret="$TS_OAUTH_CLIENT_SECRET" \
		--dry-run=client -o yaml | $KUBECTL_CMD apply -f -
	log "Tailscale operator secrets created"
fi

log "Kubernetes secrets deployed!"
echo ""

# Deploy host secrets (for NixOS services)
if [[ -n "${DEPLOY_HOST:-}" ]]; then
	log "Creating host secrets on $DEPLOY_HOST..."

	$SSH_CMD "sudo mkdir -p /var/secrets && sudo chmod 700 /var/secrets"

	# Tailscale auth key (optional, for host SSH access)
	if [[ -n "${TAILSCALE_AUTHKEY:-}" ]]; then
		echo "$TAILSCALE_AUTHKEY" | $SSH_CMD "sudo tee /var/secrets/tailscale-authkey > /dev/null"
		$SSH_CMD "sudo chmod 600 /var/secrets/tailscale-authkey"
	fi

	# Tailscale OAuth (for k8s operator)
	echo "$TS_OAUTH_CLIENT_ID" | $SSH_CMD "sudo tee /var/secrets/ts-oauth-client-id > /dev/null"
	echo "$TS_OAUTH_CLIENT_SECRET" | $SSH_CMD "sudo tee /var/secrets/ts-oauth-client-secret > /dev/null"
	$SSH_CMD "sudo chmod 600 /var/secrets/ts-oauth-client-id /var/secrets/ts-oauth-client-secret"

	# JuiceFS env file
	cat <<EOF | $SSH_CMD "sudo tee /var/secrets/juicefs.env > /dev/null"
JUICEFS_BUCKET=$JUICEFS_BUCKET
AWS_ACCESS_KEY_ID=$DO_SPACES_ACCESS_KEY
AWS_SECRET_ACCESS_KEY=$DO_SPACES_SECRET_KEY
EOF
	$SSH_CMD "sudo chmod 600 /var/secrets/juicefs.env"

	log "Host secrets deployed!"
else
	warn "DEPLOY_HOST not set, skipping host secrets"
	warn "To deploy host secrets, run: DEPLOY_HOST=user@host ./scripts/deploy-secrets.sh"
fi

echo ""
log "Done! Secrets deployed successfully."
echo ""
echo "Next steps:"
echo "  1. Apply k8s manifests: kubectl apply -f infra/k8s/"
echo "  2. Deploy NixOS config: nixos-rebuild switch --flake ./infra/nixos#netclode"
