# Netclode

## Rules

- Always ask before running `git push`
- Prefer breaking changes over backwards compatibility (no `reserved` fields in protos, etc.)

## Deployment

Server is configured in `.env` as `DEPLOY_HOST` (Tailscale hostname or IP).

### Infrastructure Provisioning (Ansible)

The server infrastructure is managed with Ansible in `infra/ansible/`. Secrets are automatically read from `.env` at the repo root.

```bash
cd infra/ansible

# Full infrastructure deployment
DEPLOY_HOST=your-server-ip ansible-playbook playbooks/site.yaml

# Fetch kubeconfig for local kubectl access
DEPLOY_HOST=your-server-ip ansible-playbook playbooks/fetch-kubeconfig.yaml

# Deploy only k8s manifests (fast updates)
DEPLOY_HOST=your-server-ip ansible-playbook playbooks/k8s-only.yaml
```

See `infra/ansible/README.md` for detailed documentation.

### Rollout after CI

After pushing changes, CI builds Docker images. To deploy:

```bash
# Wait for CI to complete
gh run watch

# Rollout control-plane
make rollout-control-plane

# Rollout agent (drains warm pool to pick up new image)
make rollout-agent
```

### Manual kubectl access

Use the `netclode` kubectl context (configured via Tailscale):

```bash
kubectl --context netclode -n netclode get pods
kubectl --context netclode -n netclode logs -l app=control-plane -f
kubectl --context netclode apply -f infra/k8s/control-plane.yaml
```

Or SSH to the server:

```bash
ssh root@netclode-host
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl -n netclode get pods
```

## Architecture

- **control-plane**: API server, Connect protocol handler, session manager
- **agent**: Runs inside sandboxes created by control-plane (not a separate deployment)

## Tailscale Hostnames

- `netclode-host` - The server itself
- `netclode-control-plane-ingress` - Control plane HTTPS endpoint (via Tailscale Ingress)

## Debugging Sessions with CLI

The `netclode` CLI (`clients/cli/`) is available for debugging sessions.

Set the control-plane URL:

```bash
# Get the ingress hostname
kubectl --context netclode -n netclode get ingress control-plane -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'

# Export it
export NETCLODE_URL=https://$(kubectl --context netclode -n netclode get ingress control-plane -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
```

### List sessions

```bash
go run ./clients/cli sessions list
```

Output shows session ID (first column), name, status, repo, message count, and timestamps:
```
ID            NAME                            STATUS   REPO                  MSGS  CREATED  ACTIVE
9f7c8e64-c84  Haptic Feedback App Impleme...  ready    angristan/netclode    2     10h ago  10h ago
05965814-225  Protocol Review Request         paused   angristan/netclode    4     10h ago  8h ago
```

### Inspect a session

```bash
# Get session details (use ID from sessions list)
go run ./clients/cli sessions get <session-id>

# View chat history (user prompts and assistant responses)
go run ./clients/cli messages <session-id>
go run ./clients/cli messages <session-id> -n 5  # Last 5 messages

# View agent events (tool calls, file changes, commands)
go run ./clients/cli events <session-id>
go run ./clients/cli events <session-id> -n 100  # Last 100 events
go run ./clients/cli events <session-id> --kind tool_start  # Filter by event type
go run ./clients/cli events <session-id> --kind file_change

# Stream events in real-time (useful for watching active sessions)
go run ./clients/cli events tail <session-id>

# Delete a session
go run ./clients/cli sessions delete <session-id>

# Pause a session (stops container, preserves workspace)
go run ./clients/cli sessions pause <session-id>

# Resume a paused session
go run ./clients/cli sessions resume <session-id>
```

### JSON output

All commands support `--json` for machine-readable output:

```bash
go run ./clients/cli sessions list --json
go run ./clients/cli events <session-id> --json
```

### Event types

- `tool_start`, `tool_end` - Tool invocations (Read, Edit, Bash, etc.)
- `file_change` - File created/edited/deleted
- `command_start`, `command_end` - Shell commands with exit codes
- `thinking` - Agent reasoning content
- `repo_clone` - Repository clone progress
- `port_exposed` - Port exposed for preview

## GPU Setup (Ollama)

Enable NVIDIA GPU support for local model inference:

```bash
cd infra/ansible
DEPLOY_HOST=your-server NVIDIA_ENABLED=true OLLAMA_ENABLED=true \
  ansible-playbook playbooks/site.yaml
```

### Secure Boot (Two-Step Installation)

If your server has Secure Boot enabled, GPU setup requires **physical access** for MOK enrollment:

1. **First run** - Installs driver, queues MOK key, shows instructions
2. **Reboot + MOK screen** - Approve the key at the blue UEFI screen (physical access required)
3. **Second run** - Completes setup (container toolkit, device plugin, Ollama)

```bash
# Check Secure Boot status
ssh root@netclode-host mokutil --sb-state

# Check if driver loads
ssh root@netclode-host modprobe nvidia && echo "OK" || echo "FAILED - need MOK enrollment"
```

See `infra/ansible/README.md` for detailed Secure Boot instructions.

### GPU Monitoring

```bash
# Quick status
ssh root@netclode-host nvidia-smi

# Live monitoring (refresh every 1s)
ssh root@netclode-host nvidia-smi -l 1

# Pretty TUI (nvtop)
ssh root@netclode-host nvtop

# From inside Ollama pod
kubectl --context netclode -n netclode exec -it deploy/ollama -- nvidia-smi
```

### Ollama Management

```bash
# Pull a model
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama pull qwen2.5-coder:32b

# List models
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama list

# Check Ollama logs
kubectl --context netclode -n netclode logs -l app=ollama -f
```

See `infra/ansible/README.md` for full GPU setup documentation.
