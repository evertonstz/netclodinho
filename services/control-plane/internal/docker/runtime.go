// Package docker provides a k8s.Runtime implementation backed by the
// Docker Engine API via github.com/moby/moby/client.
//
// Phase 1: struct, config wiring, daemon Ping.
// Phase 2 (plan 01): primitives — name helpers, label constants, ready-channel
// machinery, GetPVCName, and ensureImage (inspect-then-pull, DEVX-03).
// Phase 2 (plan 02): CreateSandbox, DeleteSandbox, DeletePVC — container lifecycle.
package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
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

	// execTimeout bounds a single one-shot Exec (WR-03). BoxLite's Exec is a bounded
	// one-shot; without an independent bound a command that never closes its output
	// stream (daemonizes, blocks on stdin) would wedge StdCopy indefinitely when the
	// caller passes context.Background().
	execTimeout = 60 * time.Second
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
		// DEVX-01: fold a bounded tail of the container's startup logs into the timeout
		// error so a "agent crashed on boot" / "token never arrived" failure is diagnosable.
		// captureStartupLogs is best-effort — it returns "" (never errors) if logs are
		// unavailable, so the timeout error is always well-formed.
		logs := r.captureStartupLogs(ctx, sessionID)
		return "", fmt.Errorf("timed out waiting for agent (session %s); recent container logs:\n%s", sessionID, logs)
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
// resolveAgentCPURL returns the control-plane URL the agent uses to call back.
// It returns the configured DockerAgentCPURL verbatim when set (D-02); otherwise it
// defaults to the compose service DNS name http://control-plane:<Port>. Unlike
// BoxLite's autoDetectCPURL, no network probe is needed on the compose network — the
// default is a static DNS name. Daemon-free so it is unit-testable.
func (r *Runtime) resolveAgentCPURL() string {
	if url := strings.TrimSpace(r.cfg.DockerAgentCPURL); url != "" {
		return url
	}
	return fmt.Sprintf("http://control-plane:%d", r.cfg.Port)
}

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

	// Agent reachability (D-02/D-03): inject the control-plane URL and SESSION_ID so
	// the agent can call back over the compose network. AGENT_SESSION_TOKEN already
	// arrives from the manager (docker mode). CONTROL_PLANE_URL defaults to the compose
	// service DNS name; SESSION_ID is idempotent (already set by the manager).
	env["CONTROL_PLANE_URL"] = r.resolveAgentCPURL()
	env["SESSION_ID"] = sessionID

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

// GetStatus reports accurate tri-state per D-02 — the BoxLite Exists:info.Running quirk
// is explicitly forbidden.
//
// running  → Exists:true, Ready:true, ServiceFQDN=containerName(sessionID)
// stopped  → Exists:true, Ready:false  (must NOT return Exists:false — manager reconciliation
//
//	treats "exists but not ready" as PAUSED, see manager.go ~264-270)
//
// not-found → Exists:false (cerrdefs.IsNotFound branch only)
func (r *Runtime) GetStatus(ctx context.Context, sessionID string) (*k8s.SandboxStatusInfo, error) {
	res, err := r.client.ContainerInspect(ctx, containerName(sessionID), dockerclient.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return &k8s.SandboxStatusInfo{Exists: false}, nil
		}
		return nil, fmt.Errorf("inspect container: %w", err)
	}
	if res.Container.State != nil && res.Container.State.Running {
		return &k8s.SandboxStatusInfo{
			Exists:      true,
			Ready:       true,
			ServiceFQDN: containerName(sessionID),
		}, nil
	}
	// Stopped but exists — D-02: must return Exists:true so manager reconciliation marks PAUSED.
	return &k8s.SandboxStatusInfo{Exists: true, Ready: false}, nil
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

// ListSandboxes queries the Docker daemon via labels (D-05 — daemon is source of truth).
// All:true is required so stopped containers are included; this enables the manager's
// Initialize reconciliation to correctly mark paused sessions across restarts (D-02/D-05).
// Filters on both labelManagedBy AND labelNetwork (D-11) so two Netclode stacks on one
// daemon never adopt each other's containers.
func (r *Runtime) ListSandboxes(ctx context.Context) ([]k8s.SandboxInfo, error) {
	f := make(dockerclient.Filters).
		Add("label", labelManagedBy+"=netclode").
		Add("label", labelNetwork+"="+r.cfg.DockerNetwork)

	res, err := r.client.ContainerList(ctx, dockerclient.ContainerListOptions{
		All:     true, // include stopped containers (D-05, pause/resume tri-state)
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}

	out := make([]k8s.SandboxInfo, 0, len(res.Items))
	for _, c := range res.Items {
		sessionID := c.Labels[labelSessionID]
		if sessionID == "" {
			continue // skip containers with missing session-id label
		}
		out = append(out, k8s.SandboxInfo{
			SessionID:   sessionID,
			ServiceFQDN: containerName(sessionID),
			Ready:       c.State == "running",
		})
	}
	return out, nil
}

// Exec runs a one-shot command in the live sandbox container and returns the buffered
// stdout, stderr, and exit code (TERM-02). It matches BoxLite's ExecResult shaping
// (boxlite/runtime.go:463-471): command + args are passed straight through as argv with
// NO sh -c wrapping (D-06 — no shell-injection surface, T-03-04).
//
// The exec is non-TTY, so its output stream is multiplexed with 8-byte frame headers and
// MUST be demuxed with stdcopy.StdCopy — a raw io.Copy garbles it (D-06). The hijacked
// connection is closed on every path via defer (D-07), and the stream is drained to
// completion before ExecInspect so the exit code is finalized (D-07).
//
// Method names are the verified moby client v0.5.0 post-split names ExecCreate /
// ExecAttach / ExecInspect — NOT the pre-split ContainerExec* names. ExecAttach itself
// starts the exec (postHijacked), so no separate ExecStart call is needed.
//
// The exec is bounded by an independent execTimeout (WR-03): a wedged command that never
// closes its output stream would otherwise block StdCopy forever when the caller passes
// context.Background(). On timeout the derived context is cancelled, unblocking the drain.
func (r *Runtime) Exec(ctx context.Context, sessionID string, command string, args ...string) (*k8s.ExecResult, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := append([]string{command}, args...) // argv passthrough, no shell wrapping (D-06)

	created, err := r.client.ExecCreate(ctx, containerName(sessionID), dockerclient.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		TTY:          false, // D-06: non-TTY buffered one-shot
	})
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	resp, err := r.client.ExecAttach(ctx, created.ID, dockerclient.ExecAttachOptions{TTY: false})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close() // D-07: runs on every path (success and error) — no leaked hijack

	var stdout, stderr bytes.Buffer
	// D-06: demux the multiplexed non-TTY stream (raw io.Copy would garble it).
	// D-07: drain fully before ExecInspect so the exit code is finalized.
	if _, err := stdcopy.StdCopy(&stdout, &stderr, resp.Reader); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("exec stream demux: %w", err)
	}
	_ = resp.CloseWrite() // D-07: half-close the write side explicitly after drain

	ins, err := r.client.ExecInspect(ctx, created.ID, dockerclient.ExecInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec inspect: %w", err)
	}

	return &k8s.ExecResult{
		ExitCode: ins.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// sensitiveEnvKeyFragments identifies container env var names whose *values* must be
// scrubbed from any startup-log output before it is logged or returned in an error.
// Matched case-insensitively as substrings (T-03-06 information disclosure). Agent
// runtimes frequently print their environment or a startup banner on boot, so a live
// AGENT_SESSION_TOKEN or ANTHROPIC_API_KEY can otherwise leak verbatim into control-plane
// logs via the WaitForReady timeout diagnostic.
var sensitiveEnvKeyFragments = []string{"TOKEN", "KEY", "SECRET", "PASSWORD"}

// redactSecrets replaces each secret value in secrets with "***" wherever it appears in s.
// Empty secret values are skipped (replacing "" would corrupt the whole string).
func redactSecrets(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		s = strings.ReplaceAll(s, secret, "***")
	}
	return s
}

// collectSensitiveEnvValues inspects the container and returns the values of any env var
// whose name matches sensitiveEnvKeyFragments, so they can be scrubbed from log output.
// Best-effort: returns whatever it can, plus the config-level ANTHROPIC_API_KEY as a floor
// so redaction still occurs even if the inspect fails.
func (r *Runtime) collectSensitiveEnvValues(ctx context.Context, sessionID string) []string {
	var secrets []string
	if r.cfg != nil && r.cfg.AnthropicAPIKey != "" {
		secrets = append(secrets, r.cfg.AnthropicAPIKey)
	}
	if r.client == nil {
		return secrets
	}
	res, err := r.client.ContainerInspect(ctx, containerName(sessionID), dockerclient.ContainerInspectOptions{})
	if err != nil || res.Container.Config == nil {
		return secrets // best-effort — fall back to config-level floor
	}
	for _, kv := range res.Container.Config.Env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, value := kv[:eq], kv[eq+1:]
		if value == "" {
			continue
		}
		upper := strings.ToUpper(name)
		for _, frag := range sensitiveEnvKeyFragments {
			if strings.Contains(upper, frag) {
				secrets = append(secrets, value)
				break
			}
		}
	}
	return secrets
}

// captureStartupLogs returns a bounded tail of the container's combined stdout+stderr
// startup logs for the given session (DEVX-01, D-08). It is fully best-effort: it returns
// "" on ANY error (missing container, log-fetch failure, demux error) so a caller — the
// WaitForReady timeout branch — is never failed because logs could not be fetched. This
// mirrors the cerrdefs.IsNotFound tolerance style used elsewhere in this file.
//
// The agent container is non-TTY, so its log stream is multiplexed exactly like the exec
// stream and must be demuxed with the same stdcopy.StdCopy (a raw read is garbled). The
// Tail is bounded to 50 lines (D-08 discretion) to limit both log volume and the
// information disclosure surface (T-03-06).
//
// Secret redaction (WR-02, T-03-06): the agent container is started with sensitive env
// (AGENT_SESSION_TOKEN, ANTHROPIC_API_KEY, ...). If the agent echoes its environment on
// boot those values would otherwise be copied verbatim into the returned/logged timeout
// error. Values of sensitively-named env vars are scrubbed to "***" before returning.
//
// Stream labeling (WR-05): stdout and stderr are demuxed into separate buffers and
// emitted as labeled sections. Preserving the stdout/stderr distinction is often the key
// signal for a boot failure — "crashed on boot" (stderr) vs "printed usage" (stdout).
func (r *Runtime) captureStartupLogs(ctx context.Context, sessionID string) string {
	if r.client == nil {
		return "" // best-effort — no daemon client (e.g. daemon-free Runtime literal)
	}
	rc, err := r.client.ContainerLogs(ctx, containerName(sessionID), dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "50", // D-08: bounded tail
	})
	if err != nil {
		return "" // best-effort — never fail the caller
	}
	defer rc.Close()

	// Demux into separate buffers so the stdout/stderr distinction is preserved (WR-05).
	var stdout, stderr bytes.Buffer
	_, _ = stdcopy.StdCopy(&stdout, &stderr, rc)

	var out bytes.Buffer
	if stdout.Len() > 0 {
		out.WriteString("[stdout]\n")
		out.Write(stdout.Bytes())
		if stdout.Bytes()[stdout.Len()-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	if stderr.Len() > 0 {
		out.WriteString("[stderr]\n")
		out.Write(stderr.Bytes())
	}

	// Scrub secret env values before the logs escape into an error/log sink (WR-02).
	return redactSecrets(out.String(), r.collectSensitiveEnvValues(ctx, sessionID))
}

// ---------------------------------------------------------------------------
// CleanupOrphans — startup-time orphan removal (D-06, D-07, D-08, DEVX-02).
// ---------------------------------------------------------------------------

// CleanupOrphans removes managed containers (and their workspace volumes) whose
// sessionID is not a key in knownSessionIDs.
//
// Must be called once at startup — after manager.Initialize loads all storage sessions —
// via a type assertion in main.go (D-08: startup only, no periodic sweep).
// The k8s.Runtime interface is NOT modified; this is a concrete *Runtime method.
//
// Orphan definition (D-06): a managed-labeled container in this stack's network scope
// whose session-id label has no record in persistent storage.
//
// Individual removal failures are logged and skipped (log-and-continue per D-06/D-07);
// a stuck orphan must never abort the control plane startup (T-02-08 mitigated).
func (r *Runtime) CleanupOrphans(ctx context.Context, knownSessionIDs map[string]bool) error {
	sandboxes, err := r.ListSandboxes(ctx)
	if err != nil {
		return fmt.Errorf("list sandboxes for orphan cleanup: %w", err)
	}

	for _, sb := range sandboxes {
		if knownSessionIDs[sb.SessionID] {
			continue // known session — keep
		}
		slog.Info("Docker: removing orphan container", "sessionID", sb.SessionID)

		// Remove container (Force:true handles both running and stopped orphans).
		if _, err := r.client.ContainerRemove(ctx, containerName(sb.SessionID), dockerclient.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			slog.Warn("Docker: failed to remove orphan container", "sessionID", sb.SessionID, "error", err)
			// log-and-continue — attempt volume removal regardless
		}

		// Remove workspace volume (D-07: orphan cleanup removes container AND volume).
		if _, err := r.client.VolumeRemove(ctx, volumeName(sb.SessionID), dockerclient.VolumeRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			slog.Warn("Docker: failed to remove orphan volume", "sessionID", sb.SessionID, "error", err)
			// log-and-continue — a stuck volume never aborts startup (T-02-08)
		}
	}
	return nil
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

