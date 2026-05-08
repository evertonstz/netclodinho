// Package boxlite provides a k8s.Runtime implementation backed by the
// vendored BoxLite Go SDK (github.com/boxlite-ai/boxlite/sdks/go).
//
// The SDK is vendored so we can keep its source and the native library in
// lockstep with the runtime behavior expected by the control-plane.
package boxlite

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	boxlitesdk "github.com/boxlite-ai/boxlite/sdks/go"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
)

var ErrNotSupported = errors.New("operation not supported in BoxLite v0.8.2 runtime mode")

const (
	agentDefaultImage   = "ghcr.io/angristan/netclode-agent:latest"
	defaultControlPlane = "http://control-plane:3000"
	boxNamePrefix       = "netclode-"
)

// BuildSecretsAndAllowNet derives the BoxLite secret substitution rules and
// the network allowlist for a given SDK type.
func BuildSecretsAndAllowNet(sdkType pb.SdkType, realSecrets map[string]string) (secrets []boxlitesdk.Secret, allowNet []string) {
	seen := map[string]bool{}
	for _, m := range sdkAllowedMappings(sdkType) {
		val := realSecrets[m.secretKey]
		if val == "" {
			val = m.placeholder
		}
		secrets = append(secrets, boxlitesdk.Secret{
			Name:        m.secretKey,
			Value:       val,
			Placeholder: m.placeholder,
			Hosts:       m.hosts,
		})
		for _, h := range m.hosts {
			if !seen[h] {
				seen[h] = true
				allowNet = append(allowNet, h)
			}
		}
	}
	return secrets, allowNet
}

type secretMapping struct {
	secretKey   string
	placeholder string
	hosts       []string
}

type sandboxCreateSpec struct {
	existingBoxName string
	sdkType         pb.SdkType
	boxEnv          map[string]string
	secrets         []boxlitesdk.Secret
	allowNet        []string
	diskSizeGb      int
	cpus            int
	memoryMB        int
}

func sdkAllowedMappings(sdkType pb.SdkType) []secretMapping {
	switch sdkType {
	case pb.SdkType_SDK_TYPE_OPENCODE:
		return []secretMapping{
			{"anthropic", "NETCLODE_PLACEHOLDER_anthropic", []string{"api.anthropic.com"}},
			{"openai", "NETCLODE_PLACEHOLDER_openai", []string{"api.openai.com"}},
			{"mistral", "NETCLODE_PLACEHOLDER_mistral", []string{"api.mistral.ai"}},
			{"opencode", "NETCLODE_PLACEHOLDER_opencode", []string{"openrouter.ai", "api.openrouter.ai", "api.opencode.ai"}},
			{"openrouter", "NETCLODE_PLACEHOLDER_openrouter", []string{"openrouter.ai", "api.openrouter.ai"}},
			{"zai", "NETCLODE_PLACEHOLDER_zai", []string{"open.bigmodel.cn"}},
			{"github_copilot_oauth_access", "NETCLODE_PLACEHOLDER_github_copilot_oauth_access", []string{"api.github.com", "api.githubcopilot.com", "api.individual.githubcopilot.com", "copilot-proxy.githubusercontent.com"}},
			{"github_copilot_oauth_refresh", "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh", []string{"api.github.com", "api.githubcopilot.com", "api.individual.githubcopilot.com", "copilot-proxy.githubusercontent.com"}},
		}
	case pb.SdkType_SDK_TYPE_COPILOT:
		return []secretMapping{
			{"github_copilot", "NETCLODE_PLACEHOLDER_github_copilot", []string{"api.github.com", "copilot-proxy.githubusercontent.com", "api.individual.githubcopilot.com"}},
			{"anthropic", "NETCLODE_PLACEHOLDER_anthropic", []string{"api.anthropic.com"}},
		}
	case pb.SdkType_SDK_TYPE_CODEX:
		return []secretMapping{
			// API mode: OPENAI_API_KEY env
			{"codex_access", "NETCLODE_PLACEHOLDER_openai", []string{"api.openai.com"}},
			// OAuth mode: tokens written to ~/.codex/auth.json, substituted in-flight
			{"codex_oauth_access", "NETCLODE_PLACEHOLDER_codex_oauth_access", []string{"api.openai.com"}},
			{"codex_oauth_id", "NETCLODE_PLACEHOLDER_codex_oauth_id", []string{"api.openai.com"}},
			{"codex_oauth_refresh", "NETCLODE_PLACEHOLDER_codex_oauth_refresh", []string{"api.openai.com"}},
		}
	default:
		return []secretMapping{{"anthropic", "NETCLODE_PLACEHOLDER_anthropic", []string{"api.anthropic.com"}}}
	}
}

// Runtime implements k8s.Runtime using the embedded BoxLite Go SDK.
type Runtime struct {
	cfg         *config.Config
	rt          *boxlitesdk.Runtime
	realKeys    map[string]string
	tokenIssuer TokenIssuer

	readyMu       sync.Mutex
	readyChannels map[string][]chan struct{}
	boxMu         sync.RWMutex
	boxes         map[string]*boxlitesdk.Box
}

// TokenIssuer is the subset of session.Manager the runtime needs.
type TokenIssuer interface {
	IssueDockerToken(sessionID string) string
}

// NewRuntime creates an embedded BoxLite runtime.
func NewRuntime(cfg *config.Config, issuer TokenIssuer) (*Runtime, error) {
	homeDir := cfg.EffectiveBoxliteHomeDir()
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return nil, fmt.Errorf("create boxlite home dir %s: %w", homeDir, err)
	}

	// Clear OCI image cache on startup so mutable tags like :tip are re-pulled.
	// BoxLite caches images by tag name and never re-pulls once cached.
	for _, dir := range []string{"images", "db"} {
		p := filepath.Join(homeDir, dir)
		if err := os.RemoveAll(p); err != nil {
			slog.Warn("Failed to clear BoxLite cache", "path", p, "error", err)
		} else {
			slog.Info("Cleared BoxLite image cache", "path", p)
		}
	}

	rt, err := boxlitesdk.NewRuntime(boxlitesdk.WithHomeDir(homeDir))
	if err != nil {
		return nil, fmt.Errorf("create boxlite runtime: %w", err)
	}

	r := &Runtime{
		cfg:           cfg,
		rt:            rt,
		realKeys:      buildRealKeys(cfg),
		tokenIssuer:   issuer,
		readyChannels: make(map[string][]chan struct{}),
		boxes:         make(map[string]*boxlitesdk.Box),
	}

	slog.Info("BoxLite embedded runtime initialised", "homeDir", homeDir, "version", boxlitesdk.Version())
	return r, nil
}

func buildRealKeys(cfg *config.Config) map[string]string {
	return map[string]string{
		"anthropic":                    cfg.AnthropicAPIKey,
		"openai":                       cfg.OpenAIAPIKey,
		"mistral":                      cfg.MistralAPIKey,
		"opencode":                     cfg.OpenCodeAPIKey,
		"openrouter":                   cfg.OpenRouterAPIKey,
		"zai":                          cfg.ZaiAPIKey,
		"github_copilot":               cfg.GitHubCopilotToken,
		"github_copilot_oauth_access":  cfg.GitHubCopilotOAuthAccessToken,
		"github_copilot_oauth_refresh": cfg.GitHubCopilotOAuthRefreshToken,
		"codex_access":                 cfg.CodexAccessToken,
		// codex_oauth_* are per-session — injected via env prefix at CreateSandbox time
	}
}

// Close shuts down the embedded runtime gracefully.
func (r *Runtime) Close() {
	if r.rt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = r.rt.Shutdown(ctx, 30*time.Second)
	_ = r.rt.Close()
}

func (r *Runtime) boxName(sessionID string) string { return boxNamePrefix + sessionID }

func (r *Runtime) getBox(sessionID string) (*boxlitesdk.Box, bool) {
	r.boxMu.RLock()
	defer r.boxMu.RUnlock()
	b, ok := r.boxes[sessionID]
	return b, ok
}

func (r *Runtime) setBox(sessionID string, box *boxlitesdk.Box) {
	r.boxMu.Lock()
	defer r.boxMu.Unlock()
	r.boxes[sessionID] = box
}

func (r *Runtime) deleteBox(sessionID string) {
	r.boxMu.Lock()
	defer r.boxMu.Unlock()
	delete(r.boxes, sessionID)
}

func (r *Runtime) watchReadyCh(sessionID string) <-chan struct{} {
	ch := make(chan struct{})
	r.readyMu.Lock()
	r.readyChannels[sessionID] = append(r.readyChannels[sessionID], ch)
	r.readyMu.Unlock()
	return ch
}

func (r *Runtime) buildSandboxCreateSpec(sessionID string, env map[string]string, resources *k8s.SandboxResourceConfig) sandboxCreateSpec {
	token := r.tokenIssuer.IssueDockerToken(sessionID)
	envCopy := make(map[string]string, len(env))
	for k, v := range env {
		envCopy[k] = v
	}

	spec := sandboxCreateSpec{
		existingBoxName: envCopy[k8s.ExistingPVCEnvKey],
		sdkType:         sdkTypeFromEnv(envCopy),
	}
	delete(envCopy, k8s.ExistingPVCEnvKey)

	// Extract per-session secrets injected by the manager via a reserved env prefix.
	// These are merged with global realKeys so BoxLite can substitute them in-flight.
	// The prefix keys are never forwarded to the guest environment.
	const perSessionPrefix = "_BOXLITE_SESSION_SECRET_"
	perSessionKeys := map[string]string{}
	for k, v := range envCopy {
		if strings.HasPrefix(k, perSessionPrefix) {
			perSessionKeys[strings.TrimPrefix(k, perSessionPrefix)] = v
		}
	}
	mergedKeys := make(map[string]string, len(r.realKeys)+len(perSessionKeys))
	for k, v := range r.realKeys {
		mergedKeys[k] = v
	}
	for k, v := range perSessionKeys {
		mergedKeys[k] = v
	}

	spec.secrets, _ = BuildSecretsAndAllowNet(spec.sdkType, mergedKeys)
	spec.allowNet = []string{}

	// Resolve the control-plane URL the agent will use to call back.
	// Priority: explicit config > auto-detected outbound IP > fallback k8s DNS name.
	cpURL := strings.TrimSpace(r.cfg.BoxliteAgentCPURL)
	if cpURL == "" {
		cpURL = autoDetectCPURL(r.cfg.Port)
	}

	spec.boxEnv = make(map[string]string, len(envCopy)+len(spec.secrets)+3)
	for k, v := range envCopy {
		if strings.HasPrefix(k, perSessionPrefix) {
			continue // never forward internal secret carriers to the guest
		}
		spec.boxEnv[k] = v
	}
	for _, secret := range spec.secrets {
		if shouldExposeGuestPlaceholderEnv(secret.Name) {
			spec.boxEnv[envKeyForSecret(secret.Name)] = secret.Placeholder
		}
	}
	spec.boxEnv["AGENT_SESSION_TOKEN"] = token
	spec.boxEnv["SESSION_ID"] = sessionID
	spec.boxEnv["CONTROL_PLANE_URL"] = cpURL

	spec.diskSizeGb = r.cfg.BoxliteDefaultDiskSizeGb
	if resources != nil && resources.DiskSizeGb > 0 {
		spec.diskSizeGb = resources.DiskSizeGb
	}
	if spec.diskSizeGb <= 0 {
		spec.diskSizeGb = 20
	}
	if resources != nil {
		if resources.VCPUs > 0 {
			spec.cpus = int(resources.VCPUs)
		}
		if resources.MemoryMB > 0 {
			spec.memoryMB = int(resources.MemoryMB)
		}
	}

	return spec
}

// CreateSandbox creates and starts a BoxLite box for the session.
func (r *Runtime) CreateSandbox(ctx context.Context, sessionID string, env map[string]string, resources *k8s.SandboxResourceConfig) error {
	if r.tokenIssuer == nil {
		return fmt.Errorf("token issuer not configured")
	}

	spec := r.buildSandboxCreateSpec(sessionID, env, resources)

	opts := []boxlitesdk.BoxOption{
		boxlitesdk.WithName(r.boxName(sessionID)),
		boxlitesdk.WithDiskSizeGb(spec.diskSizeGb),
		// Keep persisted box metadata + QCOW2 disk across Stop/Start so pause/resume works.
		// Full session deletion uses ForceRemove in DeletePVC.
		boxlitesdk.WithAutoRemove(false),
		boxlitesdk.WithNetwork(boxlitesdk.NetworkSpec{Mode: boxlitesdk.NetworkModeEnabled, AllowNet: spec.allowNet}),
		boxlitesdk.WithWorkDir("/agent"),
	}
	for k, v := range spec.boxEnv {
		opts = append(opts, boxlitesdk.WithEnv(k, v))
	}
	for _, secret := range spec.secrets {
		opts = append(opts, boxlitesdk.WithSecret(secret))
	}
	if spec.cpus > 0 {
		opts = append(opts, boxlitesdk.WithCPUs(spec.cpus))
	}
	if spec.memoryMB > 0 {
		opts = append(opts, boxlitesdk.WithMemory(spec.memoryMB))
	}

	if spec.existingBoxName != "" {
		if existingBox, err := r.rt.Get(ctx, spec.existingBoxName); err == nil && existingBox != nil {
			slog.Info("BoxLite: restarting existing box", "sessionID", sessionID, "boxName", spec.existingBoxName)
			if err := existingBox.Start(ctx); err != nil {
				_ = existingBox.Close()
				return fmt.Errorf("box restart: %w", err)
			}
			r.setBox(sessionID, existingBox)
			slog.Info("BoxLite: box restarted", "sessionID", sessionID, "boxID", existingBox.ID(), "name", existingBox.Name())
			return nil
		}
	}

	slog.Info("BoxLite: creating box", "sessionID", sessionID, "image", r.cfg.AgentImage, "sdkType", spec.sdkType, "allowNet", spec.allowNet, "diskSizeGb", spec.diskSizeGb, "cpus", spec.cpus, "memoryMB", spec.memoryMB)
	box, err := r.rt.Create(ctx, r.cfg.AgentImage, opts...)
	if err != nil {
		return fmt.Errorf("box create: %w", err)
	}

	if err := box.Start(ctx); err != nil {
		_ = box.Close()
		_ = r.rt.ForceRemove(context.Background(), box.ID())
		return fmt.Errorf("box start: %w", err)
	}

	r.setBox(sessionID, box)
	slog.Info("BoxLite: box started", "sessionID", sessionID, "boxID", box.ID(), "name", box.Name())
	return nil
}

func (r *Runtime) WaitForReady(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	ch := r.watchReadyCh(sessionID)
	select {
	case <-ch:
		return r.boxName(sessionID), nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for agent (session %s)", sessionID)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *Runtime) WatchSandboxReady(sessionID string, callback k8s.SandboxReadyCallback) {
	ch := r.watchReadyCh(sessionID)
	go func() {
		<-ch
		callback(sessionID, r.boxName(sessionID), nil)
	}()
}

func (r *Runtime) NotifyAgentReady(sessionID string) {
	r.readyMu.Lock()
	defer r.readyMu.Unlock()
	for _, ch := range r.readyChannels[sessionID] {
		close(ch)
	}
	delete(r.readyChannels, sessionID)
}

func (r *Runtime) DeleteSandbox(ctx context.Context, sessionID string) error {
	box, ok := r.getBox(sessionID)
	if !ok {
		// Best-effort fallback by name.
		box, _ = r.rt.Get(ctx, r.boxName(sessionID))
	}
	if box == nil {
		return nil
	}
	defer box.Close()
	if err := box.Stop(ctx); err != nil {
		slog.Warn("BoxLite: stop failed", "sessionID", sessionID, "error", err)
	}
	// Keep the box record and QCOW2 disk for pause/resume. Full session deletion
	// performs ForceRemove from DeletePVC.
	r.deleteBox(sessionID)
	return nil
}

func (r *Runtime) DeletePVC(ctx context.Context, sessionID string) error {
	box, err := r.rt.Get(ctx, r.boxName(sessionID))
	if err != nil || box == nil {
		return nil
	}
	defer box.Close()
	if err := r.rt.ForceRemove(ctx, box.ID()); err != nil {
		return fmt.Errorf("remove box disk: %w", err)
	}
	r.deleteBox(sessionID)
	return nil
}

func (r *Runtime) DeletePVCByName(_ context.Context, _ string) error {
	return nil
}

func (r *Runtime) DeleteSecret(_ context.Context, _ string) error { return nil }

func (r *Runtime) GetStatus(ctx context.Context, sessionID string) (*k8s.SandboxStatusInfo, error) {
	box, ok := r.getBox(sessionID)
	if !ok {
		var err error
		box, err = r.rt.Get(ctx, r.boxName(sessionID))
		if err != nil || box == nil {
			return &k8s.SandboxStatusInfo{Exists: false}, nil
		}
	}
	info, err := box.Info(ctx)
	if err != nil {
		return &k8s.SandboxStatusInfo{Exists: false}, nil
	}
	return &k8s.SandboxStatusInfo{
		Exists:      info.Running,
		Ready:       info.Running,
		ServiceFQDN: r.boxName(sessionID),
	}, nil
}

func (r *Runtime) ListSandboxes(ctx context.Context) ([]k8s.SandboxInfo, error) {
	infos, err := r.rt.ListInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("list boxes: %w", err)
	}
	out := make([]k8s.SandboxInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, k8s.SandboxInfo{
			SessionID:   sessionIDFromBoxName(info.Name),
			ServiceFQDN: info.Name,
			Ready:       info.State == boxlitesdk.StateRunning,
		})
	}
	return out, nil
}

func (r *Runtime) ConfigureNetwork(_ context.Context, sessionID string, _ bool) error {
	slog.Warn("BoxLite: ConfigureNetwork is a no-op in v0.8.2", "sessionID", sessionID)
	return nil
}

func (r *Runtime) ConfigureTailnetAccess(_ context.Context, sessionID string, _ bool) error {
	slog.Warn("BoxLite: ConfigureTailnetAccess is a no-op in v0.8.2", "sessionID", sessionID)
	return nil
}

func (r *Runtime) DeleteNetworkRestriction(_ context.Context, _ string) error { return nil }

func (r *Runtime) CreateVolumeSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: snapshots not yet exposed in BoxLite Go SDK; QCOW2 backend is ready", ErrNotSupported)
}

func (r *Runtime) WaitForSnapshotReady(_ context.Context, _, _ string, _ time.Duration) error {
	return fmt.Errorf("%w: snapshots not yet exposed in BoxLite Go SDK; QCOW2 backend is ready", ErrNotSupported)
}

func (r *Runtime) DeleteVolumeSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: snapshots not yet exposed in BoxLite Go SDK; QCOW2 backend is ready", ErrNotSupported)
}

func (r *Runtime) ListVolumeSnapshots(_ context.Context, _ string) ([]k8s.VolumeSnapshotInfo, error) {
	return nil, fmt.Errorf("%w: snapshots not yet exposed in BoxLite Go SDK; QCOW2 backend is ready", ErrNotSupported)
}

func (r *Runtime) RestoreFromSnapshot(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: snapshots not yet exposed in BoxLite Go SDK; QCOW2 backend is ready", ErrNotSupported)
}

func (r *Runtime) CreatePVCFromSnapshot(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: snapshots not yet exposed in BoxLite Go SDK; QCOW2 backend is ready", ErrNotSupported)
}

func (r *Runtime) WaitForRestoreJob(_ context.Context, _, _ string, _ time.Duration) error {
	return nil
}

func (r *Runtime) GetPVCName(_ context.Context, sessionID string) (string, error) {
	return r.boxName(sessionID), nil
}

func (r *Runtime) VerifyAgentToken(_ context.Context, _ string, _ []string) (string, error) {
	return "", fmt.Errorf("%w: use Manager.LookupDockerToken instead", ErrNotSupported)
}

func (r *Runtime) EnsureSessionAnchor(_ context.Context, _ string) error      { return nil }
func (r *Runtime) DeleteSessionAnchor(_ context.Context, _ string) error      { return nil }
func (r *Runtime) AddSessionAnchorToPVC(_ context.Context, _, _ string) error { return nil }

func (r *Runtime) CreateSandboxClaim(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("%w: warm pool is not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) WaitForClaimBound(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("%w: warm pool is not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) GetSandboxByName(_ context.Context, _ string) (*k8s.Sandbox, error) {
	return nil, fmt.Errorf("%w: not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) GetSessionIDByPodName(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) GetSessionIDByPodIP(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) LabelSandbox(_ context.Context, _, _ string) error    { return nil }
func (r *Runtime) DeleteSandboxClaim(_ context.Context, _ string) error { return nil }
func (r *Runtime) ListSandboxClaims(_ context.Context) ([]k8s.SandboxClaimInfo, error) {
	return nil, nil
}
func (r *Runtime) CreateSandboxService(_ context.Context, _ string) error {
	return fmt.Errorf("%w: not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) DeleteSandboxService(_ context.Context, _ string) error    { return nil }
func (r *Runtime) ListTailscaleServices(_ context.Context) ([]string, error) { return nil, nil }
func (r *Runtime) ExposePort(_ context.Context, _ string, _ int) error {
	return fmt.Errorf("%w: not supported in BoxLite mode", ErrNotSupported)
}
func (r *Runtime) UnexposePort(_ context.Context, _ string, _ int) error { return nil }

func sdkTypeFromEnv(env map[string]string) pb.SdkType {
	switch env["SDK_TYPE"] {
	case "SDK_TYPE_OPENCODE":
		return pb.SdkType_SDK_TYPE_OPENCODE
	case "SDK_TYPE_COPILOT":
		return pb.SdkType_SDK_TYPE_COPILOT
	case "SDK_TYPE_CODEX":
		return pb.SdkType_SDK_TYPE_CODEX
	default:
		return pb.SdkType_SDK_TYPE_CLAUDE
	}
}

func shouldExposeGuestPlaceholderEnv(name string) bool {
	switch name {
	case "github_copilot_oauth_access", "github_copilot_oauth_refresh",
		"codex_oauth_access", "codex_oauth_id", "codex_oauth_refresh":
		return false
	default:
		return true
	}
}

func envKeyForSecret(name string) string {
	switch name {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai", "codex_access":
		return "OPENAI_API_KEY"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "opencode":
		return "OPENCODE_API_KEY"
	case "zai":
		return "ZAI_API_KEY"
	case "github_copilot", "github_copilot_oauth_access", "github_copilot_oauth_refresh":
		return "GITHUB_COPILOT_TOKEN"
	default:
		return strings.ToUpper(name) + "_API_KEY"
	}
}

func sessionIDFromBoxName(name string) string {
	return strings.TrimPrefix(name, boxNamePrefix)
}

func extractHost(rawURL string) string {
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

var _ k8s.Runtime = (*Runtime)(nil)

// autoDetectCPURL returns the control-plane URL the agent inside a BoxLite VM
// should use to connect back. It probes the outbound IP of this process
// (i.e. the IP gvproxy will NAT through when the guest makes outbound TCP
// connections) and combines it with the configured port.
//
// With --network host this returns the server's actual LAN IP, making the
// URL stable regardless of what IP the server happens to have.
func autoDetectCPURL(port int) string {
	ip := outboundIP()
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		slog.Warn("BoxLite: could not detect outbound IP, falling back to default CP URL")
		return defaultControlPlane
	}
	if port <= 0 {
		port = 3000
	}
	return fmt.Sprintf("http://%s:%d", ip, port)
}

// outboundIP returns the local IP address the OS would use for outbound
// internet connections. It works by "dialing" a public address over UDP —
// no packets are actually sent.
func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
