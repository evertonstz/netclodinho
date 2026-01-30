# SDK Support

Netclode supports multiple AI agent SDKs, allowing you to choose the best provider for your workflow. Each SDK has different capabilities, authentication methods, and model options.

## Overview

| SDK | Provider | Authentication | Key Features |
|-----|----------|----------------|--------------|
| Claude Agent | Anthropic | API key | Extended thinking, native tools, session persistence |
| OpenCode | Multi-provider | API keys | Anthropic, OpenAI, Mistral support |
| Copilot | GitHub / Anthropic | GitHub token or API key | Premium quota tracking, billing multipliers |
| Codex | OpenAI | API key or ChatGPT OAuth | Reasoning effort levels, thread persistence |

## Claude Agent SDK (Default)

The Claude Agent SDK provides direct integration with Anthropic's Claude models via `@anthropic-ai/claude-agent-sdk`.

### Authentication

Requires `ANTHROPIC_API_KEY` environment variable.

### Models

- **claude-opus-4-5-20251101** (default) - Most capable, supports extended thinking
- **claude-sonnet-4-0** - Balanced performance/cost

### Features

- Extended thinking with streaming
- Native tool support (Read, Write, Edit, Bash, Glob, Grep, WebSearch, WebFetch)
- Session persistence across pause/resume
- Interrupt support with abort controller

### Configuration

```bash
# .env
ANTHROPIC_API_KEY=sk-ant-api03-xxx
```

## OpenCode SDK

OpenCode provides multi-provider support through the OpenCode CLI in server mode.

### Authentication

Supports multiple API keys depending on which provider you use:

```bash
# .env
ANTHROPIC_API_KEY=sk-ant-xxx     # For Anthropic models
OPENAI_API_KEY=sk-xxx            # For OpenAI models
MISTRAL_API_KEY=xxx              # For Mistral models
```

### Models

Model IDs use the format `provider/model-name`:

| Model ID | Provider | Notes |
|----------|----------|-------|
| `anthropic/claude-sonnet-4-0` | Anthropic | Default |
| `anthropic/claude-sonnet-4-5-20250514` | Anthropic | Supports thinking |
| `openai/gpt-4o` | OpenAI | |
| `mistral/mistral-large-latest` | Mistral | |

### Features

- Multi-provider support in a single SDK
- Extended thinking for Claude models (configurable budget)
- REST API + SSE event streaming
- Session persistence

### Thinking Configuration

For Claude models, thinking can be enabled with budget levels:

- **high** - 16,000 token budget
- **max** - 32,000 token budget

## GitHub Copilot SDK

The Copilot SDK uses `@github/copilot-sdk` with two backend options.

### GitHub Backend

Uses GitHub's Copilot API with your GitHub subscription.

**Authentication:** Requires a GitHub Personal Access Token with the `copilot` scope.

1. Go to https://github.com/settings/tokens?type=beta
2. Create a fine-grained PAT with Account permissions > Copilot > Read-only
3. Set the token in your environment:

```bash
# .env
GITHUB_COPILOT_TOKEN=github_pat_xxx
```

**Features:**
- Access to GitHub Copilot models (GPT-4o, Claude via Copilot)
- Premium request quota tracking
- Model billing multipliers (0.33x to 50x)
- No separate API costs (uses Copilot subscription)

**Model billing multipliers:**
| Multiplier | Examples |
|------------|----------|
| 0.33x | Base models |
| 1.0x | Standard models |
| 3.0x | GPT-4o |
| 50.0x | o1-pro |

### Anthropic Backend (BYOK)

Uses Anthropic API directly with your own API key. No GitHub subscription required.

**Authentication:**

```bash
# .env
ANTHROPIC_API_KEY=sk-ant-xxx
```

**Models:**
- `claude-sonnet-4-20250514`
- `claude-3-5-sonnet-20241022`
- `claude-3-5-haiku-20241022`

### Checking Copilot Status

The `get_copilot_status` API returns authentication and quota information:

```json
{
  "auth": {
    "is_authenticated": true,
    "auth_type": "user",
    "login": "username"
  },
  "quota": {
    "used": 15,
    "limit": 300,
    "remaining": 285,
    "reset_at": "2025-02-01T00:00:00Z"
  }
}
```

## OpenAI Codex SDK

The Codex SDK uses `@openai/codex-sdk` for OpenAI's coding agent.

### API Key Mode

Standard OpenAI API authentication:

```bash
# .env
OPENAI_API_KEY=sk-xxx
```

### ChatGPT OAuth Mode

Use your ChatGPT subscription instead of API credits. Authenticate using the CLI:

```bash
netclode auth codex
```

This opens a browser flow and outputs tokens to add to your `.env`:

```bash
# .env (from auth codex output)
CODEX_ACCESS_TOKEN=eyJ...
CODEX_REFRESH_TOKEN=...
CODEX_ID_TOKEN=eyJ...
```

### Models

- `codex-mini-latest` - Fast, efficient
- `gpt-5-codex` - Most capable

### Reasoning Effort

Codex supports configurable reasoning effort levels that control how much "thinking" the model does:

| Level | Description |
|-------|-------------|
| `minimal` | Fastest, least reasoning |
| `low` | Light reasoning |
| `medium` | Balanced (default) |
| `high` | More thorough reasoning |
| `xhigh` | Maximum reasoning depth |

Higher reasoning effort increases response quality but also latency and token usage.

### Features

- Thread-based conversation persistence
- Structured item types (command_execution, file_change, reasoning)
- Full sandbox access (`danger-full-access` mode)
- Web search capability

## Listing Available Models

Use the `list_models` API to get available models for an SDK:

```bash
# Via CLI (not yet implemented)
# Or via iOS app model picker
```

The response includes model metadata:

```json
{
  "models": [
    {
      "id": "claude-sonnet-4-20250514",
      "name": "Claude Sonnet 4",
      "provider": "anthropic",
      "capabilities": ["chat", "vision", "code"]
    },
    {
      "id": "gpt-4o",
      "name": "GPT-4o",
      "billing_multiplier": 3.0,
      "capabilities": ["chat", "vision"]
    }
  ]
}
```

## Session Configuration

When creating a session, specify the SDK and model:

```bash
# Claude (default)
netclode sessions create --repo owner/repo --sdk claude

# OpenCode with specific model
netclode sessions create --repo owner/repo --sdk opencode --model anthropic/claude-sonnet-4-5-20250514

# Copilot with GitHub backend
netclode sessions create --repo owner/repo --sdk copilot --model claude-sonnet-4.5

# Codex with reasoning effort
netclode sessions create --repo owner/repo --sdk codex --model codex-mini-latest
```

Use `--repo` multiple times to clone multiple repositories in a single session.

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

Netclode supports running local LLMs via Ollama with GPU acceleration. This requires:
1. NVIDIA GPU on the host
2. Ollama deployment enabled in Ansible

### Infrastructure Setup

See [infra/ansible/README.md](/infra/ansible/README.md#gpu-support-optional) for complete setup instructions including:
- NVIDIA driver installation (with Secure Boot support)
- Container toolkit and device plugin
- Ollama deployment with GPU access

Quick setup:

```bash
cd infra/ansible

# Enable GPU + Ollama
DEPLOY_HOST=your-server NVIDIA_ENABLED=true OLLAMA_ENABLED=true \
  ansible-playbook playbooks/site.yaml
```

### Pulling Models

```bash
# Pull a model
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama pull qwen2.5-coder:32b

# List available models
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama list
```

### Using Ollama in Sessions

Create sessions with the `opencode` SDK and `ollama/` model prefix:

```bash
netclode sessions create --repo owner/repo --sdk opencode --model ollama/qwen2.5:7b-instruct
```

Or via iOS app: Select "OpenCode" SDK and choose an Ollama model from the picker.

### Available Models

Models are listed in the iOS app model picker once Ollama is enabled. The control-plane fetches available models from Ollama's `/api/tags` endpoint.

Recommended models for 16GB VRAM:

| Model | Command | Use Case |
|-------|---------|----------|
| Qwen 2.5 Coder 32B | `ollama pull qwen2.5-coder:32b-instruct-q4_K_M` | Best coding |
| DeepSeek Coder V2 16B | `ollama pull deepseek-coder-v2:16b` | Fast coding |
| Mistral 7B | `ollama pull mistral:7b-instruct` | Fast general |

### Configuration

Ollama is configured with:

| Setting | Value | Description |
|---------|-------|-------------|
| `OLLAMA_NUM_CTX` | 16384 | Context window size |
| `OLLAMA_KEEP_ALIVE` | 24h | Keep model loaded |
| `OLLAMA_NUM_PARALLEL` | 1 | Concurrent requests |
| `OLLAMA_FLASH_ATTENTION` | 1 | Enable flash attention |

### GPU Monitoring

```bash
# Quick status
ssh root@netclode-host nvidia-smi

# Live monitoring
ssh root@netclode-host nvidia-smi -l 1

# Pretty TUI
ssh root@netclode-host nvtop
```

### Limitations

**Tool calling is unreliable with local models.** While models can generate responses, structured tool calls (file reads, bash commands) often fail or produce malformed JSON. This is a fundamental limitation of smaller local models compared to cloud APIs.

For production agentic workloads requiring reliable tool use, use cloud providers (Anthropic, OpenAI).
