#!/bin/bash
set -e

# Ensure workspace directories exist and are owned by agent
mkdir -p /workspace/.docker /workspace/.mise /workspace/.cache /workspace/.local
chown -R agent:agent /workspace

# Start Docker daemon in background with overlay2, data on JuiceFS
echo "[entrypoint] Starting Docker daemon..."
dockerd --storage-driver=overlay2 --data-root=/workspace/.docker &

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
