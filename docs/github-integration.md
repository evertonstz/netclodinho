# GitHub Integration

Netclode can automatically clone repositories and push commits using GitHub App authentication. This provides secure, scoped access to your repositories with short-lived tokens.

## Overview

When you create a session with a repository:
1. Control-plane generates a short-lived (1 hour) GitHub installation token
2. The token is scoped to the specific repository and requested permissions
3. The sandbox clones the repository on startup
4. The agent can push commits back to GitHub (if write access is granted)

## Setup

### 1. Create a GitHub App

1. Go to **GitHub Settings** > **Developer settings** > **GitHub Apps** > **New GitHub App**
   
   Direct link: https://github.com/settings/apps/new

2. Fill in the app details:

   | Field | Value |
   |-------|-------|
   | **GitHub App name** | `Netclode` (or any unique name) |
   | **Homepage URL** | Your Netclode URL or `https://github.com` |
   | **Webhook** | Uncheck "Active" (not needed) |

3. Set **Repository permissions**:

   | Permission | Access | Purpose |
   |------------|--------|---------|
   | **Actions** | Read-only | View workflow runs and CI status |
   | **Contents** | Read and write | Clone repos and push commits |
   | **Metadata** | Read-only (required) | Basic repo info |
   | **Pull requests** | Read and write | Create and manage PRs |
   | **Workflows** | Read and write | Modify GitHub Actions workflow files |

   > Note: Even if you only need read access for some sessions, the app needs write permission for Contents, Pull requests, and Workflows. Individual session tokens are scoped down to read-only when requested.

4. Set **Where can this GitHub App be installed?**:
   - Choose "Only on this account" for personal use
   - Choose "Any account" if you want to use it across organizations

5. Click **Create GitHub App**

### 2. Generate a Private Key

1. After creating the app, scroll down to **Private keys**
2. Click **Generate a private key**
3. A `.pem` file will be downloaded - keep this safe!

### 3. Install the App

1. Go to your app's settings page
2. Click **Install App** in the left sidebar
3. Choose the account/organization where you want to install it
4. Select which repositories the app can access:
   - **All repositories** - Access to all current and future repos
   - **Only select repositories** - Choose specific repos

5. After installation, note the **Installation ID** from the URL:
   ```
   https://github.com/settings/installations/12345678
                                              ^^^^^^^^
                                              This is the Installation ID
   ```

### 4. Configure Netclode

Add the following to your `.env` file:

```bash
# GitHub App ID (from app settings page)
GITHUB_APP_ID=123456

# Private key (paste the entire PEM content)
# Note: For multi-line values in .env, different tools handle this differently.
# If using dotenv, you can use quotes and \n for newlines, or just paste directly.
GITHUB_APP_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA...
...
-----END RSA PRIVATE KEY-----"

# Installation ID (from the installation URL)
GITHUB_INSTALLATION_ID=12345678
```

### 5. Deploy Secrets

Deploy the secrets to your Kubernetes cluster:

```bash
cd infra/ansible
ENV_FILE=../../.env DEPLOY_HOST=your-server ansible-playbook playbooks/secrets.yaml
```

Then rollout the control-plane to pick up the new configuration:

```bash
make rollout-control-plane
```

## Usage

### Creating a Session with a Repository

When creating a session via WebSocket, include the `repo` and optionally `repoAccess` fields:

```json
{
  "type": "session.create",
  "name": "my-feature",
  "repo": "owner/repo",
  "repoAccess": "write"
}
```

| Field | Description |
|-------|-------------|
| `repo` | Repository in `owner/repo` format or full URL |
| `repoAccess` | `"read"` (default) or `"write"` |

### Repository URL Formats

The following formats are supported for the `repo` field:

- `owner/repo` - Short format (recommended)
- `https://github.com/owner/repo` - Full HTTPS URL
- `https://github.com/owner/repo.git` - With .git suffix

### Access Levels

| Level | Capabilities |
|-------|--------------|
| `read` | Clone, fetch, pull |
| `write` | Clone, fetch, pull, push, create branches |

### Token Lifecycle

- Tokens are generated when the session is created
- Tokens expire after 1 hour
- If a session runs longer than 1 hour, git operations may fail until the session is resumed (which generates a new token)

### Clone Progress Events

When a session starts with a repository, the agent broadcasts progress events:

```json
// Clone starting
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

// Clone completed
{
  "type": "agent.event",
  "sessionId": "abc123",
  "event": {
    "kind": "repo_clone",
    "timestamp": "2026-01-18T22:50:05Z",
    "repo": "https://github.com/owner/repo.git",
    "stage": "done",
    "message": "Repository cloned successfully"
  }
}
```

Possible stages:
- `starting` - Clone or pull is beginning
- `done` - Operation completed (check message for details)
- `error` - Clone failed (agent continues without repo)

## How It Works

### Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│     Client      │────▶│  Control Plane   │────▶│   GitHub API    │
│                 │     │                  │     │                 │
│ session.create  │     │ 1. Sign JWT      │     │ POST /app/      │
│ {repo, access}  │     │ 2. Request token │     │ installations/  │
└─────────────────┘     │ 3. Scope to repo │     │ {id}/access_tokens
                        └────────┬─────────┘     └─────────────────┘
                                 │
                                 ▼
                        ┌──────────────────┐
                        │   Sandbox Pod    │
                        │                  │
                        │ GITHUB_TOKEN env │
                        │ GIT_REPO env     │
                        │                  │
                        │ entrypoint.sh:   │
                        │ - Configure creds│
                        │ - git clone      │
                        └──────────────────┘
```

### Token Scoping

Even though the GitHub App has broad permissions, each session token is scoped:

```json
// Request to GitHub API
POST /app/installations/{id}/access_tokens
{
  "repositories": ["specific-repo"],
  "permissions": {
    "contents": "read",  // or "write"
    "metadata": "read"
  }
}
```

This means:
- A session with `repoAccess: "read"` cannot push even if the app has write permission
- The token only works for the specific repository requested

## Troubleshooting

### "Failed to create GitHub token"

Check the control-plane logs:

```bash
kubectl --context netclode -n netclode logs -l app=control-plane -f
```

Common causes:
- Invalid private key format
- App not installed on the repository's owner/org
- Installation ID doesn't match the app

### "Repository not found" during clone

- Verify the app is installed on the repository
- Check that the repository name is correct (case-sensitive)
- For private repos, ensure the app has access

### "Permission denied" during push

- Session was created with `repoAccess: "read"` (default)
- Create a new session with `repoAccess: "write"`

### Token expired

Git operations may fail with authentication errors if the session has been running for more than 1 hour. Pause and resume the session to get a fresh token.

## Security Considerations

1. **Private Key Security**: The private key should be treated as a secret. It's stored in Kubernetes secrets and never exposed to clients.

2. **Token Scoping**: Tokens are scoped to specific repositories and permissions, limiting blast radius if a token is compromised.

3. **Short-lived Tokens**: Tokens expire after 1 hour, reducing the window of opportunity for misuse.

4. **No Token Storage**: Tokens are generated on-demand and passed to sandboxes. They're not persisted in the database.

5. **Audit Trail**: GitHub provides audit logs for all API access via the app, making it easy to track usage.
