# Netclode Agent Backend Skeleton

This directory defines the **Netclode-owned agent contract** used inside the sandbox agent service.

## Responsibility split

### `connect-client.ts`
Transport adapter only.
- Speaks protobuf/Connect with the control-plane
- Converts proto messages to Netclode runtime calls
- Converts Netclode prompt events back to protobuf responses

### `sdk/types.ts`
Product-owned internal contract.
- `NetclodeAgent`: composed runtime seen by `connect-client.ts`
- `NetclodePromptBackend`: backend prompt executor contract
- `AgentCapabilities`: explicit optional capability declaration
- `UnsupportedAgentCapabilityError`: intentional unsupported path

### `sdk/runtime.ts`
Shared composition layer.
- Wraps a prompt backend with injected collaborators
- Adds repo bootstrap, title generation, and git inspection
- Keeps backend classes focused on backend-specific execution

### `sdk/auth-materializer.ts`
Backend/provider auth preparation.
- Turns Netclode credentials into backend-specific auth artifacts
- Supports env/file-based auth materialization patterns
- Intended extension point for new backends/providers such as Pi Code

### `sdk/<backend>/adapter.ts`
Backend-specific prompt execution.
- SDK startup and shutdown
- Prompt execution
- Backend-specific event translation
- Backend-specific interrupt behavior

## Adding a new backend

A new backend should normally provide:

1. A `NetclodePromptBackend` implementation
2. An explicit `capabilities` declaration
3. A backend auth materializer (or reuse an existing one)
4. A factory entry in `sdk/factory.ts`
5. Tests for capability declaration, auth materialization, and prompt behavior

## Auth/provider model

Keep these concerns separate:

1. **Netclode credentials** - what secrets are available in session config or env
2. **Provider selection** - which provider the backend/model uses
3. **Auth materialization** - how that provider expects credentials to be written (env, auth.json, provider config, etc.)

This separation is especially important for multi-provider backends like OpenCode and future backends like Pi Code.
