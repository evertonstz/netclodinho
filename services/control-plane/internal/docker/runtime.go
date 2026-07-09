// Package docker provides a k8s.Runtime implementation backed by the
// Docker Engine API via github.com/moby/moby/client.
//
// Phase 1: struct, config wiring, daemon Ping.
// Phase 2 (plan 01): primitives — name helpers, label constants, ready-channel
// machinery, GetPVCName, and ensureImage (inspect-then-pull, DEVX-03).
// Phase 2 (plan 02): CreateSandbox, DeleteSandbox, DeletePVC — container lifecycle.
package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	dockerclient "github.com/moby/moby/client"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
)

// ErrNotSupported is returned by lifecycle methods not yet implemented.
var ErrNotSupported = errors.New("operation not supported in Docker Engine runtime")

// ---------------------------------------------------------------------------
// Name constants and helpers.
// ---------------------------------------------------------------------------

const (
	sandboxNamePrefix = "netclode-"

	// Container and volume label keys (reverse-DNS, scoped by network — D-11).
	labelManagedBy = "com.netclode.managed"
	labelSessionID = "com.netclode.session-id"
	labelNetwork   = "com.netclode.network"
)

// containerName and volumeName share the same string value.
// Docker namespaces (containers vs volumes) prevent collision (D-12).
func containerName(sessionID string) string { return sandboxNamePrefix + sessionID }
func volumeName(sessionID string) string    { return sandboxNamePrefix + sessionID }

// ---------------------------------------------------------------------------
// Runtime struct.
// ---------------------------------------------------------------------------

// Runtime implements k8s.Runtime using the Docker Engine API.
type Runtime struct {
	cfg    *config.Config
	client *dockerclient.Client

	// Ready-channel machinery — mirrors BoxLite exactly.
	readyMu       sync.Mutex
	readyChannels map[string][]chan struct{}

	// Optional in-memory sessionID → containerID cache.
	// Daemon labels are the source of truth (D-05); this is a perf optimisation only.
	// Protected by cacheMu; always fall back to ContainerInspect/ContainerList on miss.
	cacheMu        sync.RWMutex
	containerCache map[string]string // sessionID → containerID
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
	// (client.New applies opts in order; last host-setting opt takes precedence).
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

	return &Runtime{
		cfg:            cfg,
		client:         cli,
		readyChannels:  make(map[string][]chan struct{}),
		containerCache: make(map[string]string),
	}, nil
}

// Close shuts down the Docker client connection.
func (r *Runtime) Close() { _ = r.client.Close() }

// Compile-time interface assertion — fails to compile if any k8s.Runtime method is missing.
var _ k8s.Runtime = (*Runtime)(nil)

// ---------------------------------------------------------------------------
// Ready-channel machinery — mirrors BoxLite exactly (plans 01 primitives).
// ---------------------------------------------------------------------------

// watchReadyCh registers and returns a new ready channel for the session.
func (r *Runtime) watchReadyCh(sessionID string) <-chan struct{} {
	ch := make(chan struct{})
	r.readyMu.Lock()
	r.readyChannels[sessionID] = append(r.readyChannels[sessionID], ch)
	r.readyMu.Unlock()
	return ch
}

// WatchSandboxReady registers a callback that fires once NotifyAgentReady is called
// for the given sessionID. The callback receives (sessionID, containerFQDN, nil).
func (r *Runtime) WatchSandboxReady(sessionID string, callback k8s.SandboxReadyCallback) {
	ch := r.watchReadyCh(sessionID)
	go func() {
		<-ch
		callback(sessionID, containerName(sessionID), nil)
	}()
}

// NotifyAgentReady closes all ready channels registered for sessionID,
// triggering any WatchSandboxReady callbacks and unblocking any WaitForReady calls.
// Calling it for an unknown sessionID is a safe no-op.
func (r *Runtime) NotifyAgentReady(sessionID string) {
	r.readyMu.Lock()
	defer r.readyMu.Unlock()
	for _, ch := range r.readyChannels[sessionID] {
		close(ch)
	}
	delete(r.readyChannels, sessionID)
}

// WaitForReady blocks until the agent is ready, the context is cancelled, or the
// timeout elapses. On success it returns the container FQDN (containerName(sessionID)).
func (r *Runtime) WaitForReady(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	ch := r.watchReadyCh(sessionID)
	select {
	case <-ch:
		return containerName(sessionID), nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for agent (session %s)", sessionID)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// ensureImage — inspect-then-pull helper (DEVX-03, plan 01 primitive).
// ---------------------------------------------------------------------------

// ensureImage checks whether cfg.AgentImage is present locally; pulls only if missing.
// Pull uses a generous independent 10-minute timeout so a slow registry does not
// cascade into session-creation failures (D-10).
// Drains the pull response before returning so the daemon completes the pull.
func (r *Runtime) ensureImage(ctx context.Context) error {
	if _, err := r.client.ImageInspect(ctx, r.cfg.AgentImage); err == nil {
		return nil // image exists locally — skip pull (DEVX-03)
	}

	slog.Info("Docker: image not found locally, pulling", "image", r.cfg.AgentImage)
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	resp, err := r.client.ImagePull(pullCtx, r.cfg.AgentImage, dockerclient.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("image pull %s: %w", r.cfg.AgentImage, err)
	}
	// Wait drains the response to EOF so the daemon completes the pull.
	if err := resp.Wait(pullCtx); err != nil {
		return fmt.Errorf("image pull drain %s: %w", r.cfg.AgentImage, err)
	}
	slog.Info("Docker: image pull complete", "image", r.cfg.AgentImage)
	return nil
}

// ---------------------------------------------------------------------------
// GetPVCName — implemented (plan 01 primitive).
// ---------------------------------------------------------------------------

// GetPVCName returns the Docker volume name for the given sessionID.
// For the Docker runtime the volume and container share the same name (D-12).
func (r *Runtime) GetPVCName(_ context.Context, sessionID string) (string, error) {
	return containerName(sessionID), nil
}

// ---------------------------------------------------------------------------
// k8s.Runtime lifecycle methods — stubs for Phase 2 plans 02-03.
// ---------------------------------------------------------------------------

// CreateSandbox boots the agent container for a new session (SESS-01, DEVX-03).
//
// Resume path (D-03): if env contains k8s.ExistingPVCEnvKey, the container already
// exists (stopped). Start it as-is — image and env are fixed at original create time.
//
// Full-create path: ensureImage → VolumeCreate → ContainerCreate → NetworkConnect →
// ContainerStart. On NetworkConnect or ContainerStart failure, the partially-created
// container is force-removed (best-effort) before returning the wrapped error.
func (r *Runtime) CreateSandbox(ctx context.Context, sessionID string, env map[string]string, _ *k8s.SandboxResourceConfig) error {
	// Resume path (D-03): ExistingPVCEnvKey present → start existing stopped container.
	if existingName, ok := env[k8s.ExistingPVCEnvKey]; ok && existingName != "" {
		slog.Info("Docker: resuming existing container", "sessionID", sessionID, "container", existingName)
		if _, err := r.client.ContainerStart(ctx, existingName, dockerclient.ContainerStartOptions{}); err != nil {
			return fmt.Errorf("container start (resume): %w", err)
		}
		return nil
	}

	// Image availability: inspect first, pull only if missing (D-09, D-10).
	if err := r.ensureImage(ctx); err != nil {
		return err
	}

	// 1. Create named workspace volume (explicit create allows labels — audit + D-11).
	labels := map[string]string{
		labelManagedBy: "netclode",
		labelSessionID: sessionID,
		labelNetwork:   r.cfg.DockerNetwork,
	}
	if _, err := r.client.VolumeCreate(ctx, dockerclient.VolumeCreateOptions{
		Name:   volumeName(sessionID),
		Labels: labels,
	}); err != nil {
		return fmt.Errorf("volume create: %w", err)
	}

	// 2. Build env slice from map.
	envSlice := make([]string, 0, len(env))
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}

	// 3. Create container (no network at create time — NetworkConnect is done post-create).
	// RestartPolicyDisabled per D-04: control plane is the single lifecycle authority.
	// Typed Mounts not legacy Binds per CLAUDE.md. Target /agent is the agent's writable home.
	result, err := r.client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Name:  containerName(sessionID),
		Image: r.cfg.AgentImage,
		Config: &container.Config{
			Env:    envSlice,
			Labels: labels,
		},
		HostConfig: &container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: volumeName(sessionID),
					Target: "/agent",
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("container create: %w", err)
	}

	// 4. Join compose network AFTER create.
	// NetworkMode does not join a named compose network; NetworkConnect is the correct API (CLAUDE.md).
	if _, err := r.client.NetworkConnect(ctx, r.cfg.DockerNetwork, dockerclient.NetworkConnectOptions{
		Container: result.ID,
	}); err != nil {
		// Best-effort cleanup: remove the partially-created container.
		_, _ = r.client.ContainerRemove(ctx, result.ID, dockerclient.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("network connect: %w", err)
	}

	// 5. Start the container.
	if _, err := r.client.ContainerStart(ctx, result.ID, dockerclient.ContainerStartOptions{}); err != nil {
		// Best-effort cleanup: remove the partially-created container.
		_, _ = r.client.ContainerRemove(ctx, result.ID, dockerclient.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("container start: %w", err)
	}

	slog.Info("Docker: container started", "sessionID", sessionID, "containerID", result.ID)
	return nil
}

func (r *Runtime) GetStatus(_ context.Context, _ string) (*k8s.SandboxStatusInfo, error) {
	return nil, fmt.Errorf("%w: GetStatus not yet implemented (Phase 2)", ErrNotSupported)
}

// DeleteSandbox stops the container but keeps it and its volume for pause/resume (D-01).
//
// Full teardown (container + volume removal) is performed by DeletePVC.
// The manager's full-delete chain calls DeleteSandbox then DeletePVC in sequence
// (manager.go:1179-1185). Returning nil on not-found makes DeleteSandbox idempotent.
func (r *Runtime) DeleteSandbox(ctx context.Context, sessionID string) error {
	if _, err := r.client.ContainerStop(ctx, containerName(sessionID), dockerclient.ContainerStopOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil // already gone; not an error (idempotent)
		}
		return fmt.Errorf("container stop: %w", err)
	}
	slog.Info("Docker: container stopped", "sessionID", sessionID)
	return nil
}

// DeletePVC force-removes the container and its workspace volume (D-01, D-07).
//
// Force:true handles both running and stopped containers without error (Pitfall 6).
// Container removal continues to volume removal even when the container is already gone —
// the volume must always be cleaned up. Volume removal error is surfaced to the caller.
func (r *Runtime) DeletePVC(ctx context.Context, sessionID string) error {
	if _, err := r.client.ContainerRemove(ctx, containerName(sessionID), dockerclient.ContainerRemoveOptions{
		Force: true,
	}); err != nil && !cerrdefs.IsNotFound(err) {
		// Log and continue — volume must still be removed even if container removal fails.
		slog.Warn("Docker: container remove failed", "sessionID", sessionID, "error", err)
	}

	if _, err := r.client.VolumeRemove(ctx, volumeName(sessionID), dockerclient.VolumeRemoveOptions{
		Force: true,
	}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("volume remove: %w", err)
	}
	slog.Info("Docker: container and volume removed", "sessionID", sessionID)
	return nil
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
	return fmt.Errorf("%w: snapshots not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) WaitForSnapshotReady(_ context.Context, _, _ string, _ time.Duration) error {
	return fmt.Errorf("%w: snapshots not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) DeleteVolumeSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: snapshots not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) ListVolumeSnapshots(_ context.Context, _ string) ([]k8s.VolumeSnapshotInfo, error) {
	return nil, fmt.Errorf("%w: snapshots not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) RestoreFromSnapshot(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: snapshots not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) CreatePVCFromSnapshot(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: snapshots not supported in Docker Engine runtime", ErrNotSupported)
}

func (r *Runtime) VerifyAgentToken(_ context.Context, _ string, _ []string) (string, error) {
	return "", fmt.Errorf("%w: use Manager.LookupDockerToken instead", ErrNotSupported)
}

