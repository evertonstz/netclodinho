# GitHub Integration

Netclode uses a GitHub App to generate per-repo scoped tokens on demand. Each token only has access to the repos you select.

## Overview

When you create a session with repos:
1. You select repos and access level (read or write)
2. Control-plane generates a token scoped to **only those repos**
3. Sandbox clones the repos on startup
4. Agent can push commits (if write access granted)
5. Access level can be changed mid-session

## Access Levels

| Repo Selected | Access Level | Capabilities |
|---------------|--------------|--------------|
| No | N/A | No git operations |
| Yes | **Read** (default) | Clone only |
| Yes | **Write** | Clone and push |

Write access is always scoped to the selected repos only - you can't accidentally push to other repos.

## Setup

### 1. Create a GitHub App

1. Go to https://github.com/settings/apps/new
2. Fill in:
   - **Name**: `Netclode`
   - **Homepage URL**: `https://github.com/angristan/netclode`
   - **Webhook**: Check "Active" if using the [GitHub Bot](github-bot.md), otherwise uncheck
3. Set permissions:
   - Contents: **Read and write**
   - Metadata: **Read-only**
   - Pull requests: **Read and write** (optional)
   - Issues: **Read and write** (required for [GitHub Bot](github-bot.md))
4. Install scope: **Only on this account** (or Any account for multiple orgs)
5. Click **Create GitHub App**

### 2. Generate Private Key

1. After creating the app, scroll down to **Private keys**
2. Click **Generate a private key**
3. A `.pem` file will be downloaded
4. Base64 encode the key for your `.env`:
   ```bash
   cat your-app-name.2024-01-26.private-key.pem | base64 | tr -d '\n'
   ```

### 3. Install the App

1. Go to your app's settings → **Install App**
2. Choose account/org
3. Select **All repositories** (or specific ones)
4. Note the **Installation ID** from the URL: `https://github.com/settings/installations/12345678` → ID is `12345678`

### 4. Configure Environment

Add the following to your `.env` file:

```bash
# GitHub App for repository access
GITHUB_APP_ID=123456          # App ID from app settings page
GITHUB_APP_PRIVATE_KEY_B64=   # Base64-encoded private key
GITHUB_INSTALLATION_ID=12345  # From installation URL
```

### 5. Deploy Secrets

```bash
cd infra/ansible
DEPLOY_HOST=your-server ansible-playbook playbooks/secrets.yaml
make rollout-control-plane
```

## Usage

### Creating a Session with Repositories

When creating a session via Connect protocol, include the `repos` and `repo_access` fields:

```json
{
  "type": "session.create",
  "name": "my-feature",
  "repos": ["owner/repo", "owner/other"],
  "repoAccess": "write"
}
```

| Field | Values | Default |
|-------|--------|---------|
| `repos` | Repositories in `owner/repo` format or full URL | - |
| `repoAccess` | `read`, `write` | `read` |

### Repository URL Formats

The following formats are supported for each entry in `repos`:

- `owner/repo` - Short format (recommended)
- `https://github.com/owner/repo` - Full HTTPS URL
- `https://github.com/owner/repo.git` - With .git suffix

### Changing Access Level Mid-Session

**iOS App**: Session menu → tap access level → select new level

When access changes, control-plane generates a new token and the agent reconfigures git credentials immediately. Useful when you start read-only and later need to push.

### Clone Progress Events

When a session starts with repositories, the agent broadcasts progress events:

```json
{
  "type": "agent.event",
  "sessionId": "abc123",
  "event": {
    "kind": "repo_clone",
    "timestamp": "2026-01-18T22:50:00Z",
    "repo": "https://github.com/owner/repo.git",
    "stage": "starting",
    "message": "Cloning repository..."
  }
}
```

Possible stages:
- `starting` - Clone is beginning
- `done` - Clone completed successfully
- `error` - Clone failed (agent continues without repos)

## Architecture

```
┌─────────────────┐     ┌──────────────────┐
│     Client      │────▶│  Control Plane   │
│                 │     │                  │
│ session.create  │     │ 1. Generate repo-│
│ {repos, access} │     │    scoped token  │
└─────────────────┘     │    via GitHub App│
                        └────────┬─────────┘
                                 │
                                 ▼
                        ┌──────────────────┐
                        │   Sandbox Pod    │
                        │                  │
                        │ GITHUB_TOKEN env │
                        │ GIT_REPOS env    │
                        │                  │
                        │ entrypoint.sh:   │
                        │ - Configure creds│
                        │ - git clone      │
                        └──────────────────┘
```

### Mid-Session Credential Update Flow

```
┌─────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│     Client      │────▶│  Control Plane   │────▶│      Agent       │
│                 │     │                  │     │                  │
│ repo.access.    │     │ 1. Validate req  │     │ 1. Receive msg   │
│ update          │     │ 2. Generate new  │     │ 2. Reconfigure   │
│ {write}         │     │    token via App │     │    git creds     │
│                 │◀────│ 3. Send response │     │ 3. Ready for     │
│                 │     │                  │     │    push          │
└─────────────────┘     └──────────────────┘     └──────────────────┘
```

## Token Lifecycle

GitHub App tokens expire after 1 hour, scoped to only the requested repos and permissions. If a token expires during a long session, control-plane handles refresh automatically.

## Troubleshooting

**"Repository not found"** - verify the repo exists and GitHub App is installed on the owner account/org.

**"Permission denied" during push** - session has read-only access. Change to write in session menu.

**"GitHub App not configured"** - check env vars:
```bash
kubectl --context netclode -n netclode exec deploy/control-plane -- printenv | grep GITHUB_APP
```

**"Resource not accessible by integration"** - app doesn't have access to this repo. Go to https://github.com/settings/installations and add the repo.

## Security

- **Per-repo scoping** - tokens only access the specific repos requested
- **Minimal permissions** - read or write, nothing more
- **Short-lived** - tokens expire after 1 hour
- **No storage** - tokens generated on-demand, not stored in DB
- **Private key protection** - stored in k8s secrets, never exposed to clients
