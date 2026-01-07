#!/bin/bash
set -e

# Ensure directories exist and are owned by agent
# /agent is HOME (persisted on JuiceFS)
# /agent/workspace is for the user's code
# /agent/docker is for Docker data
mkdir -p /agent/workspace /agent/docker /agent/.local /agent/.cache
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

# Drop privileges and run agent
echo "[entrypoint] Starting agent as user 'agent'..."
exec su -s /bin/bash agent -c "cd /opt/agent && node agent.js"
