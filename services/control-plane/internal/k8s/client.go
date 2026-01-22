package k8s

import (
	"context"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/config"
)

// Runtime defines the interface for Kubernetes operations.
type Runtime interface {
	// Direct Sandbox operations (legacy mode)
	CreateSandbox(ctx context.Context, sessionID string, env map[string]string) error
	WaitForReady(ctx context.Context, sessionID string, timeout time.Duration) (serviceFQDN string, err error)
	WatchSandboxReady(sessionID string, callback SandboxReadyCallback)
	GetStatus(ctx context.Context, sessionID string) (*SandboxStatusInfo, error)
	DeleteSandbox(ctx context.Context, sessionID string) error
	DeletePVC(ctx context.Context, sessionID string) error
	DeleteSecret(ctx context.Context, sessionID string) error
	ListSandboxes(ctx context.Context) ([]SandboxInfo, error)

	// SandboxClaim operations (warm pool mode)
	CreateSandboxClaim(ctx context.Context, sessionID string) error
	WaitForClaimBound(ctx context.Context, sessionID string, timeout time.Duration) (sandboxName string, err error)
	GetSandboxByName(ctx context.Context, name string) (*Sandbox, error)
	GetSessionIDByPodName(ctx context.Context, podName string) (string, error)
	LabelSandbox(ctx context.Context, sandboxName string, sessionID string) error
	DeleteSandboxClaim(ctx context.Context, sessionID string) error
	ListSandboxClaims(ctx context.Context) ([]SandboxClaimInfo, error)

	// Service operations (for Tailscale preview URLs)
	CreateSandboxService(ctx context.Context, sessionID string) error
	DeleteSandboxService(ctx context.Context, sessionID string) error
	ExposePort(ctx context.Context, sessionID string, port int) error

	Close()
}

// NewRuntime creates a new Kubernetes runtime with informer-based watching.
func NewRuntime(cfg *config.Config) (Runtime, error) {
	return newK8sRuntime(cfg)
}
