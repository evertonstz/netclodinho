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

## Custom VM Resources

Sessions run in Kata Containers VMs with configurable CPU and memory.

### How It Works

1. **iOS app** shows a "Custom Resources" toggle in the new session sheet
2. User selects vCPUs (1/2/4/8) and memory (2/4/8/16 GB)
3. **Control-plane** validates against server limits and creates the sandbox
4. **Kata VM** receives full resources via annotations
5. **K8s scheduler** sees reduced requests (overcommit) for better packing

### Resource Limits

Limits are derived from host resources:

| Resource | Calculation | Example (16 CPU, 64GB host) |
|----------|-------------|----------------------------|
| Max vCPUs | 50% of host | 8 vCPUs |
| Max Memory | 25% of host, rounded to power of 2 | 16 GB |
| Default vCPUs | `DEFAULT_CPUS` env | 4 vCPUs |
| Default Memory | `DEFAULT_MEMORY_MB` env | 4096 MB |

### Overcommit

K8s requests are divided by overcommit ratio for scheduling, while Kata VMs get full resources:

```
K8s request = VM resources / overcommit_ratio
```

| Env Var | Default | Description |
|---------|---------|-------------|
| `CPU_OVERCOMMIT_RATIO` | 4 | CPU overcommit (4 = request 1/4 of actual) |
| `MEMORY_OVERCOMMIT_RATIO` | 4 | Memory overcommit |

Example with 4x overcommit:

| VM Gets | K8s Requests |
|---------|--------------|
| 8 vCPUs | 2 CPUs |
| 16 GB | 4 GB |

### Configuration

Set via environment variables on control-plane:

```bash
# View current config
kubectl --context netclode -n netclode exec deploy/control-plane -- env | grep -E "CPU|MEMORY|OVERCOMMIT"

# Update overcommit (triggers rollout)
kubectl --context netclode -n netclode set env deployment/control-plane \
  CPU_OVERCOMMIT_RATIO=4 \
  MEMORY_OVERCOMMIT_RATIO=4
```

### Bypassing Warm Pool

Sessions with custom resources (different from defaults) bypass the warm pool and create a dedicated sandbox. This is slower (~30s vs instant) but allows non-standard resource configurations.

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
