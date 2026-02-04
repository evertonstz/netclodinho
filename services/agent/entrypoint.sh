#!/bin/bash
set -e

# Trust secret-proxy CA certificate if mounted
# This allows the agent to make HTTPS requests through the MITM proxy
PROXY_CA="/usr/local/share/ca-certificates/secret-proxy.crt"
if [ -f "$PROXY_CA" ]; then
	echo "[entrypoint] Adding secret-proxy CA to trusted certificates..."
	update-ca-certificates 2>/dev/null || true
fi

# Start auth-proxy if SA token is mounted
# The auth-proxy adds Proxy-Authorization headers to requests before forwarding
# to the external secret-proxy. This enables per-session authentication.
SA_TOKEN_PATH="/var/run/secrets/proxy-auth/token"
if [ -f "$SA_TOKEN_PATH" ]; then
	echo "[entrypoint] Starting auth-proxy..."
	# The external secret-proxy URL is set via env var from sandbox template
	UPSTREAM_PROXY="${SECRET_PROXY_URL:-http://secret-proxy.netclode.svc.cluster.local:8080}"
	TOKEN_PATH="$SA_TOKEN_PATH" UPSTREAM_PROXY="$UPSTREAM_PROXY" LISTEN_ADDR=":8080" \
		/usr/local/bin/auth-proxy &
	AUTH_PROXY_PID=$!

	# Wait for auth-proxy to be ready
	echo "[entrypoint] Waiting for auth-proxy to be ready..."
	timeout=10
	while ! nc -z 127.0.0.1 8080 2>/dev/null && [ $timeout -gt 0 ]; do
		sleep 0.5
		timeout=$((timeout - 1))
	done

	if nc -z 127.0.0.1 8080 2>/dev/null; then
		echo "[entrypoint] auth-proxy ready on localhost:8080"
	else
		echo "[entrypoint] Warning: auth-proxy not responding after 5s"
	fi
else
	echo "[entrypoint] Warning: SA token not found at $SA_TOKEN_PATH, auth-proxy not started"
fi

# Ensure directories exist and are owned by agent
# /agent is HOME (persisted on JuiceFS)
# /agent/workspace is for the user's code
# /agent/docker is for Docker data
# /agent/.local/share/mise is for mise installed tools (persisted)
# /agent/.cache/mise is for mise package cache
# Note: Can't use /agent/.config - JuiceFS creates a .config file at mount root
mkdir -p /agent/workspace /agent/docker /agent/.local/share/mise /agent/.cache/mise /agent/.local/config
chown -R agent:agent /agent

# Start Docker daemon with data on JuiceFS
echo "[entrypoint] Starting Docker daemon..."
dockerd --storage-driver=vfs --data-root=/agent/docker &

# Wait for Docker socket to be ready
echo "[entrypoint] Waiting for Docker socket..."
timeout=30
while [ ! -S /var/run/docker.sock ] && [ $timeout -gt 0 ]; do
	sleep 1
	timeout=$((timeout - 1))
done

if [ ! -S /var/run/docker.sock ]; then
	echo "[entrypoint] Warning: Docker socket not available after 30s"
else
	# Make socket accessible to docker group
	chmod 666 /var/run/docker.sock
	echo "[entrypoint] Docker daemon ready"
fi

# Configure git credentials if GitHub token is provided
# This is done as root so the credential file is set up before switching to agent user
if [ -n "$GITHUB_TOKEN" ]; then
	echo "[entrypoint] Configuring git credentials..."
	# Create credentials file for agent user
	# Use .local/config instead of .config (JuiceFS uses .config at mount root)
	mkdir -p /agent/.local/config/git
	echo "https://x-access-token:${GITHUB_TOKEN}@github.com" >/agent/.git-credentials
	chown -R agent:agent /agent/.local/config /agent/.git-credentials
	chmod 600 /agent/.git-credentials
fi

# Symlink pre-installed bun cache (contains @opencode-ai/plugin)
# Symlink is instant vs cp -r which takes ~60s on JuiceFS (1400+ files)
if [ -d /opt/bun-cache ] && [ ! -e /agent/.bun ]; then
	ln -s /opt/bun-cache /agent/.bun
fi

# Symlink pre-installed OpenCode config (with node_modules)
# This makes OpenCode skip the bun add step entirely on first request
if [ -d /opt/opencode-config ] && [ ! -e /agent/.local/config/opencode ]; then
	mkdir -p /agent/.local/config
	ln -s /opt/opencode-config /agent/.local/config/opencode
	chown -h agent:agent /agent/.local/config/opencode
fi

# Drop privileges and run agent
# Preserve PATH and mise env vars for the agent user
# Include shims path so mise-installed tools are available
# Set XDG_CONFIG_HOME to avoid JuiceFS .config file at mount root
# Set NODE_EXTRA_CA_CERTS if proxy CA exists (Node.js needs this for HTTPS through MITM proxy)
echo "[entrypoint] Starting agent as user 'agent'..."
NODE_CA_ENV=""
if [ -f "$PROXY_CA" ]; then
	NODE_CA_ENV="export NODE_EXTRA_CA_CERTS=$PROXY_CA"
fi
exec su -s /bin/bash agent -c "
    export MISE_DATA_DIR=/agent/.local/share/mise
    export MISE_CACHE_DIR=/agent/.cache/mise
    export XDG_CONFIG_HOME=/agent/.local/config
    export PATH='/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:/usr/local/bin:/usr/bin:/bin'
    $NODE_CA_ENV
    cd /opt/agent && /opt/node/bin/node agent.js
"
