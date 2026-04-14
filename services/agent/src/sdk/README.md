# Netclode Agent Backend Layer

This directory defines the **Netclode-owned backend/runtime contract** used inside the sandbox agent service.

## Responsibility split

### `connect-client.ts`
Transport adapter only.
- Speaks protobuf/Connect with the control-plane
- Converts protobuf requests into Netclode runtime calls
- Converts Netclode prompt events back into protobuf responses

### `sdk/types.ts`
Product-owned internal contract.
- `NetclodeAgent`: composed runtime seen by `connect-client.ts`
- `NetclodePromptBackend`: backend prompt runner contract
- `AgentCapabilities`: explicit optional capability declaration
- `UnsupportedAgentCapabilityError`: intentional unsupported path

### `sdk/runtime.ts`
Shared composition layer.
- Wraps a prompt backend with injected collaborators
- Adds repo bootstrap, title generation, and git inspection
- Keeps backend classes focused on backend-specific execution

### `sdk/auth-materializer.ts`
Backend/provider auth preparation.
- Turns Netclode credentials into backend/provider auth artifacts
- Keeps auth file generation in one obvious source of truth
- Supports env/file-based auth materialization patterns
- Intended extension point for new backends/providers such as Pi Code

### `sdk/<backend>/adapter.ts`
Backend-specific prompt execution.
- Backend startup and shutdown
- Prompt execution
- Backend-specific event translation
- Backend-specific interrupt behavior
- Delegation to shared auth materialization when credentials must be written

## Adding a new backend

A new backend should normally provide:

1. A `NetclodePromptBackend` implementation
2. An explicit `capabilities` declaration
3. A backend/provider auth materializer (or reuse an existing one)
4. A factory entry in `sdk/factory.ts`
5. Tests for capability declaration, auth materialization, and prompt behavior

## Transport vs backend vs auth/provider

Keep these concerns separate:

1. **Transport contract** - protobuf/Connect messages between control-plane and sandbox agent
2. **Backend contract** - the Netclode-owned runtime/backend interfaces inside the agent service
3. **Auth/provider materialization** - how a backend/provider expects credentials to be resolved and written

This separation is especially important for multi-provider backends like OpenCode and future backends like Pi Code.
