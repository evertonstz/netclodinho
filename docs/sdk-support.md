# SDK Support

Netclode supports multiple coding agent SDKs. Pick the one that fits your workflow.

## Overview

| SDK | Provider | Authentication | Key Features |
|-----|----------|----------------|--------------|
| Claude Agent | Anthropic | API key | Extended thinking, native tools, session persistence |
| OpenCode | Multi-provider | API keys | Anthropic, OpenAI, Mistral support |
| Copilot | GitHub / Anthropic | GitHub token or API key | Premium quota tracking, billing multipliers |
| Codex | OpenAI | API key or ChatGPT OAuth | Reasoning effort levels, thread persistence |

## Claude Agent SDK (Default)

Direct integration with Anthropic's Claude via `@anthropic-ai/claude-agent-sdk`.

**Auth:** `ANTHROPIC_API_KEY`

**Models:**
- `claude-opus-4-5-20251101` (default) - most capable, extended thinking
- `claude-sonnet-4-0` - balanced

**Features:** Extended thinking, native tools, session persistence, interrupt support.

### Configuration

```bash
# .env
ANTHROPIC_API_KEY=sk-ant-api03-xxx
```

## OpenCode SDK

Multi-provider support through the OpenCode CLI in server mode.

**Auth:** Set whichever API keys you need:
```bash
ANTHROPIC_API_KEY=sk-ant-xxx
OPENAI_API_KEY=sk-xxx
MISTRAL_API_KEY=xxx
```

**Models:** Format is `provider/model-name`:
- `anthropic/claude-sonnet-4-0` (default)
- `anthropic/claude-sonnet-4-5-20250514` (with thinking)
- `openai/gpt-4o`
- `mistral/mistral-large-latest`

**Thinking budgets:** `high` (16k tokens), `max` (32k tokens)

## GitHub Copilot SDK

Uses `@github/copilot-sdk` with two backend options.

### GitHub Backend

Uses your Copilot subscription. Create a fine-grained PAT at https://github.com/settings/tokens?type=beta with Copilot read-only permission.

```bash
GITHUB_COPILOT_TOKEN=github_pat_xxx
```

Includes quota tracking and billing multipliers (0.33x to 50x depending on model).

### Anthropic Backend (BYOK)

Use Anthropic API directly without GitHub subscription:

```bash
ANTHROPIC_API_KEY=sk-ant-xxx
```

## OpenAI Codex SDK

Uses `@openai/codex-sdk`.

### API Key Mode

```bash
OPENAI_API_KEY=sk-xxx
```

### ChatGPT OAuth Mode

Use your ChatGPT subscription instead of API credits:

```bash
netclode auth codex
```

Opens browser flow and outputs tokens for your `.env`.

### Models

- `codex-mini-latest` - fast
- `gpt-5-codex` - most capable

### Reasoning Effort

Controls how much "thinking" the model does: `minimal`, `low`, `medium` (default), `high`, `xhigh`. Higher = better quality but more latency.

## Session Configuration

Specify SDK and model when creating a session:

```bash
netclode sessions create --repo owner/repo --sdk claude
netclode sessions create --repo owner/repo --sdk opencode --model anthropic/claude-sonnet-4-5-20250514
netclode sessions create --repo owner/repo --sdk copilot
netclode sessions create --repo owner/repo --sdk codex --model codex-mini-latest
```

Or use the iOS app model picker.

## Environment Variables Reference

| Variable | SDK | Description |
|----------|-----|-------------|
| `ANTHROPIC_API_KEY` | Claude, OpenCode, Copilot (BYOK) | Anthropic API key |
| `OPENAI_API_KEY` | OpenCode, Codex | OpenAI API key |
| `MISTRAL_API_KEY` | OpenCode | Mistral API key |
| `GITHUB_COPILOT_TOKEN` | Copilot | GitHub PAT with copilot scope |
| `CODEX_ACCESS_TOKEN` | Codex | ChatGPT OAuth access token |
| `CODEX_ID_TOKEN` | Codex | ChatGPT OAuth ID token |
| `CODEX_REFRESH_TOKEN` | Codex | ChatGPT OAuth refresh token |

## Local Models with Ollama

Run local LLMs with GPU acceleration. Requires NVIDIA GPU and Ollama deployment.

### Setup

```bash
cd infra/ansible
DEPLOY_HOST=your-server NVIDIA_ENABLED=true OLLAMA_ENABLED=true \
  ansible-playbook playbooks/site.yaml
```

See [infra/ansible/README.md](/infra/ansible/README.md#gpu-support-optional) for full setup.

### Usage

```bash
# Pull a model
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama pull qwen2.5-coder:32b

# Use in session
netclode sessions create --repo owner/repo --sdk opencode --model ollama/qwen2.5:7b-instruct
```

Models show up in the iOS app picker once Ollama is running.

### Recommended models (16GB VRAM)

- `qwen2.5-coder:32b-instruct-q4_K_M` - best coding
- `deepseek-coder-v2:16b` - fast coding
- `mistral:7b-instruct` - fast general
