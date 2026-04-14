# Netclode — Docker Compose Deployment (Boxlite)

Deploy the full Netclode stack on any KVM-capable Linux server with a single `docker compose up`.
Powered by **Boxlite** — a purpose-built microVM runtime for AI agents that provides hardware-level
isolation. The compose path does not run a separate secret-proxy service; instead it uses BoxLite
host-side secret substitution for supported env/header flows and direct in-guest auth material only
for SDKs that require file-based credentials.

## How it works

```
┌─────────────────────────────────────────────────────────────────┐
│  docker-compose stack                                           │
│                                                                 │
│  tailscale ──────── exposes control-plane on your tailnet       │
│  redis ──────────── session state                               │
│  control-plane ───── API server; embeds BoxLite runtime        │
│      ┌─────────────────────────────────────────┐               │
│      │  Agent VM (KVM microVM)                 │               │
│      │  ANTHROPIC_API_KEY=<placeholder>        │               │
│      │    ↓ outbound request to allowlisted    │               │
│      │      host gets real secret injected     │               │
│      │  File-based OAuth SDKs may write real   │               │
│      │  auth material inside the guest         │               │
│      │  BoxLite VM isolation + networking      │               │
│      └─────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────┘
```

**Secret model in compose mode:** Netclode uses the embedded BoxLite runtime directly. For SDKs
that send provider credentials in normal outbound HTTP(S) headers, the guest sees placeholder env
values and BoxLite performs host-side substitution for allowlisted hosts. For SDKs that require
file-based OAuth/auth stores (for example OpenCode GitHub Copilot OAuth or Codex OAuth), Netclode
writes the real auth material inside the guest because BoxLite cannot safely rewrite arbitrary auth
files.

## Prerequisites

| Requirement | Notes |
|---|---|
| Linux host (x86-64) | Ubuntu 22.04+ or Debian 12+ |
| KVM (`/dev/kvm`) | Required for Boxlite microVM isolation |
| Docker CE | Installed by Ansible playbook |
| Tailscale account | Free tier works; auth key required |

**Check KVM availability:**
```bash
ls -la /dev/kvm          # must exist
# If on a VM, check nested virtualisation:
cat /sys/module/kvm_intel/parameters/nested   # should be Y
```

**KVM-capable VPS providers:** DigitalOcean (all Droplets), Vultr (all plans), GCP (with nested-virt enabled), AWS EC2 (`.metal` instances).

## Step-by-step setup

### 1. Provision the host

```bash
DEPLOY_HOST=your-server-ip ansible-playbook infra/ansible/playbooks/docker-deploy.yaml
```

Installs Docker CE, Tailscale, verifies `/dev/kvm`, and writes an `.env` skeleton.

> The control-plane Docker build automatically runs `go run github.com/boxlite-ai/boxlite/sdks/go/cmd/setup`
> to download the BoxLite native library. If you are building the control-plane outside Docker,
> run that command once inside `services/control-plane/` first.

### 2. Configure `.env`

SSH to the server and edit `/opt/netclode/.env`:

```bash
ssh root@your-server
nano /opt/netclode/.env
```

**Minimum required:**
```bash
ANTHROPIC_API_KEY=sk-ant-...   # at least one LLM key
TS_AUTHKEY=tskey-auth-...       # Tailscale auth key (see below)
TS_HOSTNAME=netclode            # your tailnet hostname
```

### 3. Start the stack

```bash
cd /opt/netclode
docker compose up -d

# Include the GitHub bot (optional):
docker compose --profile github-bot up -d
```

That's it. No CA cert generation, no secrets.json, no Kata Containers setup.

### 4. Verify

```bash
docker compose ps                    # all services running
docker compose logs -f control-plane # watch logs
docker compose exec tailscale tailscale status  # check tailnet
```

The control-plane is reachable at `https://<TS_HOSTNAME>.<tailnet>.ts.net`.

## Tailscale auth key

1. Go to [Tailscale Admin → Keys](https://login.tailscale.com/admin/settings/keys)
2. **Generate auth key** with:
   - Reusable: **yes**
   - Tags: `tag:netclode`
   - Expiry: 90 days (or "no expiry")
3. Copy the `tskey-auth-...` value into `.env` as `TS_AUTHKEY`

## Environment variables reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `ANTHROPIC_API_KEY` | Yes (at least one) | — | Anthropic API key |
| `TS_AUTHKEY` | Yes | — | Tailscale auth key |
| `TS_HOSTNAME` | No | `netclode` | Tailnet hostname |
| `OPENAI_API_KEY` | No | — | OpenAI API key |
| `MISTRAL_API_KEY` | No | — | Mistral API key |
| `OPENCODE_API_KEY` | No | — | OpenCode API key |
| `ZAI_API_KEY` | No | — | Z.AI API key |
| `GITHUB_COPILOT_TOKEN` | No | — | GitHub Copilot PAT for env/header-based Copilot flows |
| `GITHUB_COPILOT_OAUTH_ACCESS_TOKEN` | No | — | GitHub Copilot OAuth access token for file-based OpenCode auth |
| `GITHUB_COPILOT_OAUTH_REFRESH_TOKEN` | No | — | GitHub Copilot OAuth refresh token for file-based OpenCode auth |
| `GITHUB_COPILOT_OAUTH_TOKEN_EXPIRES` | No | `0` | Unix timestamp for OAuth expiry (`0` = unknown / not tracked) |
| `BOXLITE_HOME_DIR` | No | `/var/lib/boxlite` | BoxLite home directory used by the embedded runtime |
| `BOXLITE_WORKSPACE_ROOT` | No | `/var/lib/netclode/workspaces` | Workspace directory root. **On macOS use `~/.boxlite/workspaces`** so Boxlite can reliably mount it. |
| `BOXLITE_AGENT_CP_URL` | No | auto-detected | Control-plane URL used by agents inside BoxLite VMs. Leave unset unless you need to override auto-detection. |
| `BOXLITE_PERSISTENT_DISK_SIZE_GB` | No | `0` (disabled) | Per-session persistent block disk size in GB. Set to e.g. `2` to attach a 2 GB disk per session. |
| `BOXLITE_SNAPSHOT_RETENTION` | No | `5` | Number of snapshots to keep per session before pruning oldest. |
| `BOXLITE_STARTUP_LOG_RETENTION` | No | `5` | Number of agent startup logs to keep per session. |
| `MAX_ACTIVE_SESSIONS` | No | `5` | Max concurrent sessions |
| `IDLE_TIMEOUT_MINUTES` | No | `0` (disabled) | Auto-pause idle sessions |
| `GITHUB_APP_ID` | No | — | GitHub App ID |
| `GITHUB_APP_PRIVATE_KEY_B64` | No | — | GitHub App private key (base64) |
| `GITHUB_INSTALLATION_ID` | No | — | GitHub App installation ID |
| `GITHUB_WEBHOOK_SECRET` | No | — | GitHub webhook secret |

## GitHub Bot setup

The GitHub bot is optional. Enable it with:
```bash
docker compose --profile github-bot up -d
```

To expose it publicly via Tailscale Funnel for webhooks:
```bash
docker compose exec tailscale tailscale funnel --bg 8082
```

Configure your GitHub App webhook URL to `https://<TS_HOSTNAME>.<tailnet>.ts.net:8082/webhook`.

## Workspace backup

Agent workspaces are stored on the host path configured via `BOXLITE_WORKSPACE_ROOT`
(the default is `/var/lib/netclode/workspaces`):

```bash
# List workspace directories
ls /var/lib/netclode/workspaces

# Backup all workspaces
tar czf workspaces-backup-$(date +%Y%m%d).tar.gz -C /var/lib/netclode/workspaces .

# Restore
tar xzf workspaces-backup-*.tar.gz -C /var/lib/netclode/workspaces
```

## Updating

```bash
cd /opt/netclode
git pull
docker compose build
docker compose up -d
```

## Troubleshooting

**Boxlite won't start:**
```bash
docker compose logs boxlite
ls -la /dev/kvm   # must exist and be accessible
```

**Tailscale not connecting:**
```bash
docker compose logs tailscale
# Check TS_AUTHKEY is valid and not expired
```

**Sessions not starting:**
```bash
docker compose logs control-plane | grep -i "boxlite\|sandbox\|error"
# Verify the control-plane can access /dev/kvm and the BoxLite home dir exists.
```

**Agent can't reach API / session never becomes READY:**
- Leave `BOXLITE_AGENT_CP_URL` unset unless you specifically need to override it
- Check `docker compose logs control-plane | grep -i "agent\|ready\|sandbox"`
- Verify the chosen IP is reachable from the BoxLite VM network in your environment
- Verify the API key is set in `.env` for the SDK type being used

**Session becomes READY but the model never answers:**
- Check the workspace `agent.log` for the active session under `/var/lib/netclode/workspaces/<session-id>/agent.log`
- For OpenCode + GitHub Copilot OAuth, verify `GITHUB_COPILOT_OAUTH_ACCESS_TOKEN` and `GITHUB_COPILOT_OAUTH_REFRESH_TOKEN` are set in `.env`
- Remember the compose path has **no secret-proxy**: file-based SDK auth must be materialized correctly inside the guest
- If you changed agent credential handling, rebuild the agent image and clear stale BoxLite session state before retrying

**Reset everything:**
```bash
docker compose down -v   # removes all volumes (destroys session data!)
docker compose up -d
```
