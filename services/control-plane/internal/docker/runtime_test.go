package docker

import (
	"strings"
	"testing"

	"github.com/angristan/netclode/services/control-plane/internal/config"
)

// TestNewRuntime_MissingNetwork verifies that NewRuntime returns a non-nil error
// when cfg.DockerNetwork is empty — this fail-fast requires no live daemon.
func TestNewRuntime_MissingNetwork(t *testing.T) {
	cfg := &config.Config{
		RuntimeMode:   config.RuntimeModeDocker,
		DockerNetwork: "", // empty — must fail fast
	}

	rt, err := NewRuntime(cfg)
	if err == nil {
		// Close to avoid a daemon leak in the unlikely case this somehow succeeded.
		if rt != nil {
			rt.Close()
		}
		t.Fatal("NewRuntime() should return a non-nil error when DockerNetwork is empty")
	}
	if !strings.Contains(err.Error(), "DOCKER_NETWORK") {
		t.Errorf("error message should mention DOCKER_NETWORK, got: %v", err)
	}
}

// TestNewRuntime_Ping verifies that NewRuntime succeeds when a live Docker daemon is
// reachable. The test is gated: if NewRuntime returns a connection/ping error it is
// assumed no daemon is available in the current environment and the test is skipped.
// This keeps `go test ./...` green in CI without a daemon while still exercising the
// full config→client→ping path locally on OrbStack/Linux.
func TestNewRuntime_Ping(t *testing.T) {
	cfg := &config.Config{
		RuntimeMode:   config.RuntimeModeDocker,
		DockerNetwork: "netclode_default",
		// DockerHost left empty — falls back to /var/run/docker.sock via client.FromEnv
	}

	rt, err := NewRuntime(cfg)
	if err != nil {
		// Any connection / ping error means no daemon is available; skip gracefully.
		t.Skipf("docker daemon not reachable, skipping ping test: %v", err)
	}

	// If we got here, a daemon is present and the ping succeeded.
	defer rt.Close()

	if rt.client == nil {
		t.Error("NewRuntime() returned a non-nil Runtime with a nil client")
	}
	if rt.cfg != cfg {
		t.Error("NewRuntime() did not store the provided config")
	}
}
