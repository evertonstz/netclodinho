// Package docker provides a k8s.Runtime implementation backed by the
// Docker Engine API via github.com/moby/moby/client.
//
// Phase 1: stub only — all lifecycle methods return ErrNotSupported.
// Docker daemon is Pinged at NewRuntime to validate socket/config wiring.
package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	dockerclient "github.com/moby/moby/client"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
)

// ErrNotSupported is returned by all lifecycle methods in the Phase 1 stub.
// Phase 2 will replace these with real Docker Engine API implementations.
var ErrNotSupported = errors.New("operation not supported in Docker Engine runtime (Phase 1 stub)")

// Runtime implements k8s.Runtime using the Docker Engine API.
// Phase 1: validates config/daemon wiring via Ping at startup; all lifecycle methods return ErrNotSupported.
type Runtime struct {
	cfg    *config.Config
	client *dockerclient.Client
}

// NewRuntime creates a Docker Engine runtime.
// It validates that DOCKER_NETWORK is set, constructs the Docker client (honoring DOCKER_HOST),
// and Pings the daemon to fail fast on a missing or unreachable socket.
func NewRuntime(cfg *config.Config) (*Runtime, error) {
	if cfg.DockerNetwork == "" {
		return nil, fmt.Errorf("DOCKER_NETWORK is required when RUNTIME_MODE=docker")
	}

	// Start with FromEnv so DOCKER_CERT_PATH / DOCKER_TLS_VERIFY are still honoured.
	// When an explicit DockerHost is set, append WithHost AFTER FromEnv so it wins
	// (client.New applies opts in order; last host-setting opt takes precedence — Pitfall 4).
	opts := []dockerclient.Opt{dockerclient.FromEnv}
	if cfg.DockerHost != "" {
		opts = append(opts, dockerclient.WithHost(cfg.DockerHost))
	}

	// Use client.New — the deprecated NewClientWithOpts constructor is intentionally avoided.
	// API version negotiation is on by default in v0.5.0; no extra opt is needed.
	cli, err := dockerclient.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ping validates the full client→daemon roundtrip.
	// NegotiateAPIVersion=true ensures the client downgrades to the daemon's API version
	// when the daemon is older than the client's default; this is safe and recommended.
	if _, err := cli.Ping(ctx, dockerclient.PingOptions{NegotiateAPIVersion: true}); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("ping docker daemon: %w (check DOCKER_HOST or /var/run/docker.sock mount)", err)
	}

	return &Runtime{cfg: cfg, client: cli}, nil
}

// Close shuts down the Docker client connection.
func (r *Runtime) Close() { _ = r.client.Close() }

// Compile-time interface assertion — fails to compile if any k8s.Runtime method is missing.
var _ k8s.Runtime = (*Runtime)(nil)

// ---------------------------------------------------------------------------
// k8s.Runtime lifecycle methods — Phase 2 will implement these.
// ---------------------------------------------------------------------------

func (r *Runtime) CreateSandbox(_ context.Context, _ string, _ map[string]string, _ *k8s.SandboxResourceConfig) error {
	return fmt.Errorf("%w: CreateSandbox not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) WaitForReady(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("%w: WaitForReady not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) WatchSandboxReady(_ string, _ k8s.SandboxReadyCallback) {
	// Phase 2: no-op in stub; callback is never invoked.
}

func (r *Runtime) GetStatus(_ context.Context, _ string) (*k8s.SandboxStatusInfo, error) {
	return nil, fmt.Errorf("%w: GetStatus not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) DeleteSandbox(_ context.Context, _ string) error {
	return fmt.Errorf("%w: DeleteSandbox not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) DeletePVC(_ context.Context, _ string) error {
	return fmt.Errorf("%w: DeletePVC not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) ListSandboxes(_ context.Context) ([]k8s.SandboxInfo, error) {
	return nil, fmt.Errorf("%w: ListSandboxes not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) Exec(_ context.Context, _ string, _ string, _ ...string) (*k8s.ExecResult, error) {
	return nil, fmt.Errorf("%w: Exec not yet implemented (Phase 2)", ErrNotSupported)
}

// ---------------------------------------------------------------------------
// No-ops — harmless to ignore (mirrors boxlite disposition exactly).
// ---------------------------------------------------------------------------

func (r *Runtime) DeletePVCByName(_ context.Context, _ string) error         { return nil }
func (r *Runtime) DeleteSecret(_ context.Context, _ string) error            { return nil }
func (r *Runtime) EnsureSessionAnchor(_ context.Context, _ string) error     { return nil }
func (r *Runtime) DeleteSessionAnchor(_ context.Context, _ string) error     { return nil }
func (r *Runtime) AddSessionAnchorToPVC(_ context.Context, _, _ string) error { return nil }
func (r *Runtime) LabelSandbox(_ context.Context, _, _ string) error         { return nil }
func (r *Runtime) DeleteSandboxClaim(_ context.Context, _ string) error      { return nil }
func (r *Runtime) ListSandboxClaims(_ context.Context) ([]k8s.SandboxClaimInfo, error) {
	return nil, nil
}
func (r *Runtime) DeleteSandboxService(_ context.Context, _ string) error    { return nil }
func (r *Runtime) ListTailscaleServices(_ context.Context) ([]string, error) { return nil, nil }
func (r *Runtime) UnexposePort(_ context.Context, _ string, _ int) error     { return nil }
func (r *Runtime) DeleteNetworkRestriction(_ context.Context, _ string) error { return nil }
func (r *Runtime) ConfigureNetwork(_ context.Context, _ string, _ bool) error { return nil }
func (r *Runtime) ConfigureTailnetAccess(_ context.Context, _ string, _ bool) error {
	return nil
}
func (r *Runtime) WaitForRestoreJob(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}
func (r *Runtime) NotifyAgentReady(_ string) {}

// ---------------------------------------------------------------------------
// ErrNotSupported — warm pool, snapshots, tailscale, K8s-specific operations.
// ---------------------------------------------------------------------------

func (r *Runtime) CreateSandboxClaim(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("%w: warm pool not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) WaitForClaimBound(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("%w: warm pool not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) GetSandboxByName(_ context.Context, _ string) (*k8s.Sandbox, error) {
	return nil, fmt.Errorf("%w: not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) GetSessionIDByPodName(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) GetSessionIDByPodIP(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) CreateSandboxService(_ context.Context, _ string) error {
	return fmt.Errorf("%w: not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) ExposePort(_ context.Context, _ string, _ int) error {
	return fmt.Errorf("%w: not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) CreateVolumeSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: snapshots not supported in Docker Engine runtime (Phase 1 stub)", ErrNotSupported)
}

func (r *Runtime) WaitForSnapshotReady(_ context.Context, _, _ string, _ time.Duration) error {
	return fmt.Errorf("%w: snapshots not supported in Docker Engine runtime (Phase 1 stub)", ErrNotSupported)
}

func (r *Runtime) DeleteVolumeSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: snapshots not supported in Docker Engine runtime (Phase 1 stub)", ErrNotSupported)
}

func (r *Runtime) ListVolumeSnapshots(_ context.Context, _ string) ([]k8s.VolumeSnapshotInfo, error) {
	return nil, fmt.Errorf("%w: snapshots not supported in Docker Engine runtime (Phase 1 stub)", ErrNotSupported)
}

func (r *Runtime) RestoreFromSnapshot(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: snapshots not supported in Docker Engine runtime (Phase 1 stub)", ErrNotSupported)
}

func (r *Runtime) CreatePVCFromSnapshot(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: snapshots not supported in Docker Engine runtime (Phase 1 stub)", ErrNotSupported)
}

func (r *Runtime) GetPVCName(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: GetPVCName not yet implemented (Phase 2)", ErrNotSupported)
}

func (r *Runtime) VerifyAgentToken(_ context.Context, _ string, _ []string) (string, error) {
	return "", fmt.Errorf("%w: use Manager.LookupDockerToken instead", ErrNotSupported)
}
