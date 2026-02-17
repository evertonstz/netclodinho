# GitHub Bot

The github-bot service (`services/github-bot/`) receives GitHub webhooks and creates Netclode sessions to handle them. It reuses the same GitHub App as repo access (see [github-integration.md](github-integration.md)).

## Triggers

### @netclode mentions

Comment `@netclode <request>` on any PR or issue. The bot:

1. Verifies the commenter has write/admin access to the repo
2. Posts a "looking into it" comment
3. Creates a sandbox session with the repo cloned
4. Sends a prompt with PR diff (or issue body), comment thread, and the user's request
5. Updates the comment with the agent's response
6. Deletes the session

Works on PR comments, issue comments, and PR review comments.

### Dependency update PRs

When Dependabot or Renovate opens/updates a PR, the bot automatically:

1. Creates a sandbox session
2. Sends a prompt instructing the agent to:
   - Inspect what changed in the dependency (module cache, git diff between tags)
   - Find impacted code paths in the repo
   - Check CI status (or run tests locally if no CI)
   - Render a verdict: **Safe to merge**, **Needs review**, or **Issues found**
3. Posts the review as a comment

Triggered by `dependabot[bot]` and `renovate[bot]`/`renovate` authors only.

### /review-dep-bump command

Comment `@netclode /review-dep-bump` on any PR to manually trigger the dependency review workflow, regardless of PR author.

## Configuration

Environment variables (set in k8s deployment):

| Variable | Default | Description |
|----------|---------|-------------|
| `CONTROL_PLANE_URL` | (required) | Control-plane gRPC endpoint |
| `REDIS_URL` | (required) | Redis for dedup + session tracking |
| `GITHUB_APP_ID` | (required) | GitHub App ID |
| `GITHUB_APP_PRIVATE_KEY` | (required) | PEM-encoded private key (raw, not base64) |
| `GITHUB_INSTALLATION_ID` | (required) | GitHub App installation ID |
| `GITHUB_WEBHOOK_SECRET` | (required) | Webhook signature verification |
| `MODEL` | `claude-opus-4-6` | LLM model |
| `SDK_TYPE` | `claude` | SDK: `claude`, `opencode`, `copilot`, `codex` |
| `MAX_CONCURRENT` | `5` | Max concurrent sessions |
| `SESSION_TIMEOUT` | `10m` | Per-session timeout |
| `PORT` | `8080` | HTTP listen port |

## Architecture

```
GitHub webhook
    │
    ▼
github-bot (POST /webhook)
    │
    ├── Verify signature
    ├── Dedup check (Redis)
    ├── Access control (GitHub API permission check)
    ├── Return 200 immediately
    │
    └── Async workflow:
        ├── Post "thinking" comment
        ├── Create session via control-plane (Connect bidi stream, h2c)
        │   └── initial_prompt sends the prompt automatically
        ├── Stream until RUNNING → READY transition
        ├── Collect last assistant message (discard intermediate narration)
        ├── Update comment with response
        └── Delete session
```

Redis is used for:
- **Webhook deduplication**: prevents processing the same delivery twice
- **In-flight session tracking**: maps `deliveryID → {sessionID, owner, repo, number, commentID}` for crash recovery

On startup, the bot recovers any orphaned sessions from Redis.

## GitHub App setup

The bot reuses the existing GitHub App. Additional requirements:

1. **Enable webhooks** in the app settings
2. **Set webhook URL** to `https://netclode-github-bot-ingress.tail527cb.ts.net/webhook`
3. **Set webhook secret** (same as `GITHUB_WEBHOOK_SECRET`)
4. **Add permissions**: `issues:write` (for posting comments)
5. **Subscribe to events**: `issue_comment`, `pull_request`, `pull_request_review_comment`

After adding new event subscriptions, the installation owner must accept updated permissions at `https://github.com/settings/installations`.

## Networking

The webhook endpoint is exposed publicly via Tailscale Funnel. The control-plane connection uses h2c (HTTP/2 cleartext) for in-cluster Connect protocol bidi streaming.

See the [Tailscale Funnel section](../infra/ansible/README.md#tailscale-funnel-public-internet-access) in the Ansible README for ACL requirements.

## Rollout

```bash
# After CI builds the image
make rollout-github-bot
```

To update the k8s manifest (env vars, resource limits):
```bash
cd infra/ansible
DEPLOY_HOST=your-server ansible-playbook playbooks/k8s-only.yaml
```
