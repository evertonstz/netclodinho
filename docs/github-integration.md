# GitHub Integration

Netclode provides secure repository access using a GitHub App that generates per-repo scoped tokens on demand. This approach provides the best security by limiting token scope to only the repository being accessed.

## Overview

When you create a session with a repository:
1. User selects a repository and access level (read or write)
2. Control-plane generates a token scoped to **only that repository** via GitHub App
3. The sandbox clones the repository on startup using that token
4. The agent can push commits back to GitHub (if write access is granted)
5. Access level can be changed mid-session if needed

## Access Levels

| Repo Selected | Access Level | Token | Capabilities |
|---------------|--------------|-------|--------------|
| No | N/A | None | No git operations |
| Yes | **Read** (default) | GitHub App token (`contents:read`) | Clone only |
| Yes | **Write** | GitHub App token (`contents:write`) | Clone and push |

**Key point**: Write access is always scoped to the selected repository only. You cannot accidentally push to other repos.

## Setup

### 1. Create a GitHub App

1. Go to https://github.com/settings/apps/new
2. Fill in the app details:
   - **GitHub App name**: `Netclode` (or your preferred name)
   - **Homepage URL**: Your deployment URL or `https://github.com/angristan/netclode`
   - **Webhook**: Uncheck "Active" (not needed)
3. Set permissions:
   - **Repository permissions**:
     - Contents: **Read and write**
     - Metadata: **Read-only**
     - Pull requests: **Read and write** (optional, for PR workflows)
4. Choose where the app can be installed:
   - **Only on this account** (simpler)
   - Or **Any account** (if you want to access repos from multiple orgs)
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

1. Go to your app's settings page
2. Click **Install App** in the sidebar
3. Choose the account/org to install on
4. Select repositories:
   - **All repositories** (recommended for full access)
   - Or select specific repos
5. After installation, note the **Installation ID** from the URL:
   - URL will be: `https://github.com/settings/installations/12345678`
   - Installation ID is `12345678`

### 4. Configure Environment

Add the following to your `.env` file:

```bash
# GitHub App for repository access
GITHUB_APP_ID=123456          # App ID from app settings page
GITHUB_APP_PRIVATE_KEY_B64=   # Base64-encoded private key
GITHUB_INSTALLATION_ID=12345  # From installation URL
```

### 5. Deploy Secrets

Deploy the secrets to your Kubernetes cluster:

```bash
cd infra/ansible
DEPLOY_HOST=your-server ansible-playbook playbooks/secrets.yaml
```

Then rollout the control-plane:

```bash
make rollout-control-plane
```

## Usage

### Creating a Session with a Repository

When creating a session via Connect protocol, include the `repo` and `repo_access` fields:

```json
{
  "type": "session.create",
  "name": "my-feature",
  "repo": "owner/repo",
  "repoAccess": "write"
}
```

| Field | Values | Default |
|-------|--------|---------|
| `repo` | Repository in `owner/repo` format or full URL | - |
| `repoAccess` | `read`, `write` | `read` |

### Repository URL Formats

The following formats are supported for the `repo` field:

- `owner/repo` - Short format (recommended)
- `https://github.com/owner/repo` - Full HTTPS URL
- `https://github.com/owner/repo.git` - With .git suffix

### Changing Access Level Mid-Session

Users can change the repository access level after a session is created:

**iOS App**: Open session menu > tap the access level item > select new level

**Protocol**: Send `UpdateRepoAccessRequest`:

```json
{
  "type": "repo.access.update",
  "sessionId": "abc123",
  "repoAccess": "write"
}
```

When access is changed:
1. Control-plane validates the request
2. Generates a new token via GitHub App with updated permissions
3. Sends `UpdateGitCredentials` message to the agent
4. Agent reconfigures git credentials immediately
5. Session metadata is updated and persisted
6. `RepoAccessUpdatedResponse` is sent back to the client

This is useful when you start a session with read-only access and later decide you need to push changes.

### Clone Progress Events

When a session starts with a repository, the agent broadcasts progress events:

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
- `error` - Clone failed (agent continues without repo)

## Architecture

```
┌─────────────────┐     ┌──────────────────┐
│     Client      │────▶│  Control Plane   │
│                 │     │                  │
│ session.create  │     │ 1. Generate repo-│
│ {repo, access}  │     │    scoped token  │
└─────────────────┘     │    via GitHub App│
                        └────────┬─────────┘
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

GitHub App installation tokens:
- Expire after **1 hour** by default
- Are scoped to **only the requested repository**
- Have **only the requested permissions** (read or write)

If a token expires during a long session, the agent may need to request a new token. The control-plane handles this automatically when credentials are updated.

## Troubleshooting

### "Repository not found" during clone

- Verify the repository exists and is accessible
- Check that the GitHub App is installed on the repo's owner account/org

### "Permission denied" during push

- Session was created with read-only access
- Change access level to write using the session menu
- Or create a new session with write access

### "GitHub App not configured"

The control-plane logs this warning when GitHub App credentials are missing. Check:

```bash
# Verify environment variables are set
kubectl --context netclode -n netclode exec deploy/control-plane -- printenv | grep GITHUB_APP
```

Ensure `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_B64`, and `GITHUB_INSTALLATION_ID` are all set.

### "Resource not accessible by integration"

The GitHub App doesn't have access to the repository. Either:
- The app isn't installed on the repo's owner account/org
- The app was installed with "Selected repositories" and this repo isn't selected

To fix:
1. Go to https://github.com/settings/installations
2. Find your Netclode app installation
3. Update repository access to include the needed repo

## Security Considerations

1. **Per-Repo Scoping**: Each token is scoped to only the specific repository being accessed. A token for `owner/repo-a` cannot access `owner/repo-b`.

2. **Minimal Permissions**: Tokens request only the permissions needed (read or write), not full access.

3. **Short-Lived Tokens**: GitHub App tokens expire after 1 hour, limiting the window of exposure if compromised.

4. **No Token Storage**: Tokens are generated on-demand and passed directly to sandboxes. They're not stored in the database.

5. **Private Key Protection**: The GitHub App private key is stored in Kubernetes secrets and never exposed to clients.

6. **Audit Trail**: GitHub provides audit logs for all API access via GitHub App tokens.
