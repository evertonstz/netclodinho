#!/bin/bash
set -e

# Ensure directories exist and are owned by agent
# /agent is HOME (persisted on JuiceFS)
# /agent/workspace is for the user's code
# /agent/docker is for Docker data
# /agent/.local/share/mise is for mise installed tools (persisted)
# /agent/.cache/mise is for mise package cache
mkdir -p /agent/workspace /agent/docker /agent/.local/share/mise /agent/.cache/mise
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
	mkdir -p /agent/.config/git
	echo "https://x-access-token:${GITHUB_TOKEN}@github.com" >/agent/.git-credentials
	chown -R agent:agent /agent/.config /agent/.git-credentials
	chmod 600 /agent/.git-credentials
fi

# Configure git credentials if GitHub token is provided
if [ -n "$GITHUB_TOKEN" ]; then
	echo "[entrypoint] Configuring git credentials..."
	mkdir -p /agent/.config/git
	echo "https://x-access-token:${GITHUB_TOKEN}@github.com" >/agent/.git-credentials
	chown -R agent:agent /agent/.config /agent/.git-credentials
	chmod 600 /agent/.git-credentials
fi

# Drop privileges and run agent
# Preserve PATH and mise env vars for the agent user
# Include shims path so mise-installed tools are available
echo "[entrypoint] Starting agent as user 'agent'..."
exec su -s /bin/bash agent -c "
    export MISE_DATA_DIR=/agent/.local/share/mise
    export MISE_CACHE_DIR=/agent/.cache/mise
    export PATH='/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:/usr/local/bin:/usr/bin:/bin'
    cd /opt/agent && /opt/node/bin/node agent.js
"
