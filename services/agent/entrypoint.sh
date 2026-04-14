#!/bin/bash
set -e

# Netclode Agent Entrypoint (Boxlite-compatible)
#
# Boxlite handles: VM isolation, secret injection (MITM), network allowlists.
# This entrypoint just sets up the workspace and starts the agent.

# Ensure directories exist and are owned by agent
# /agent is HOME (persisted via Boxlite virtiofs or volume mount)
mkdir -p /agent/workspace /agent/.local/share/mise /agent/.cache/mise /agent/.local/config /agent/.claude
# chown is best-effort: BoxLite auto-idmap handles ownership on virtiofs mounts
chown -R agent:agent /agent 2>/dev/null || true

# Disable Co-Authored-By trailer in Claude Code commits
echo '{"includeCoAuthoredBy": false}' >/agent/.claude/settings.json
chown agent:agent /agent/.claude/settings.json 2>/dev/null || true

# Configure git credentials if GitHub token is provided
if [ -n "$GITHUB_TOKEN" ]; then
	echo "[entrypoint] Configuring git credentials..."
	mkdir -p /agent/.local/config/git
	echo "https://x-access-token:${GITHUB_TOKEN}@github.com" >/agent/.git-credentials
	chown -R agent:agent /agent/.local/config /agent/.git-credentials 2>/dev/null || true
	chmod 600 /agent/.git-credentials
fi

# Symlink pre-installed bun cache
if [ -d /opt/bun-cache ] && [ ! -e /agent/.bun ]; then
	ln -s /opt/bun-cache /agent/.bun
fi

# Symlink pre-installed OpenCode config (with node_modules)
if [ -d /opt/opencode-config ] && [ ! -e /agent/.local/config/opencode ]; then
	mkdir -p /agent/.local/config
	ln -s /opt/opencode-config /agent/.local/config/opencode
	chown -h agent:agent /agent/.local/config/opencode 2>/dev/null || true
fi

# Drop privileges and run agent, persisting logs to the shared workspace
# Ensure the agent log file exists and is owned by the agent user so the VM/host can read it.
echo "[entrypoint] Starting agent as user 'agent' (logs → /agent/agent.log)..."
# Create log file with correct ownership so agent can append to it
mkdir -p /agent
touch /agent/agent.log
chown agent:agent /agent/agent.log 2>/dev/null || true
chmod 640 /agent/agent.log

# ── Startup log shipper ───────────────────────────────────────────────────────
# Capture early startup info and POST it to the control-plane immediately so
# the control-plane receives bootstrap diagnostics even when host_path mounts
# or exec/output endpoints are flaky.
ship_startup_log() {
	local cp_url="${CONTROL_PLANE_URL:-}"
	local token="${AGENT_SESSION_TOKEN:-}"
	local session_id="${SESSION_ID:-}"
	if [ -z "$cp_url" ] || [ -z "$token" ]; then
		echo "[entrypoint] skipping startup log ship: CONTROL_PLANE_URL or AGENT_SESSION_TOKEN not set"
		return
	fi
	local ts
	ts=$(date -u +"%Y%m%dT%H%M%SZ" 2>/dev/null || echo "unknown")
	# Collect a compact startup snapshot (env, key files)
	local log_content
	log_content=$(printf '==== AGENT STARTUP %s ====\n' "$ts")
	log_content+=$(printf '\n--- SESSION_ID: %s ---\n' "$session_id")
	log_content+=$(printf '\n--- ENV ---\n')
	log_content+=$(env 2>&1 | head -80 || true)
	log_content+=$(printf '\n--- node version ---\n')
	log_content+=$(/opt/node/bin/node --version 2>&1 || echo 'node not found')
	log_content+=$(printf '\n--- /agent contents ---\n')
	log_content+=$(ls -la /agent 2>&1 || echo 'no /agent')

	# Build JSON payload (escape log content)
	local escaped_log
	escaped_log=$(printf '%s' "$log_content" | sed 's/\\/\\\\/g; s/"/\\"/g; s/$/\\n/g' | tr -d '\n')
	local payload="{\"session_id\":\"${session_id}\",\"log\":\"${escaped_log}\",\"timestamp\":\"${ts}\"}"

	# POST with retries (3 attempts, 1s backoff)
	local attempt
	for attempt in 1 2 3; do
		if curl -sf --max-time 5 \
			-X POST "${cp_url}/agent-startup-log" \
			-H "Content-Type: application/json" \
			-H "Authorization: Bearer ${token}" \
			-d "$payload" > /dev/null 2>&1; then
			echo "[entrypoint] startup log shipped to control-plane (attempt $attempt)"
			return 0
		fi
		echo "[entrypoint] startup log ship failed (attempt $attempt), retrying..."
		sleep 1
	done
	echo "[entrypoint] startup log ship failed after 3 attempts — continuing"
}

# Ship startup log in background so it doesn't delay agent boot
ship_startup_log &

# Exec the agent as the non-root 'agent' user and tee stdout/stderr to /agent/agent.log
# This replaces the current process so PID 1 inside the container is the child
exec su -s /bin/bash agent -c "
    export MISE_DATA_DIR=/agent/.local/share/mise
    export MISE_CACHE_DIR=/agent/.cache/mise
    export XDG_CONFIG_HOME=/agent/.local/config
    export PATH='/agent/.local/share/mise/shims:/opt/mise/bin:/opt/node/bin:/usr/local/bin:/usr/bin:/bin'
    cd /opt/agent
    echo '==== AGENT START $(date) ====' >> /agent/agent.log
    echo '--- ENV ---' >> /agent/agent.log
    env >> /agent/agent.log
    echo '--- RUNNING agent.js ---' >> /agent/agent.log
    /opt/node/bin/node agent.js 2>&1 | tee -a /agent/agent.log
"
