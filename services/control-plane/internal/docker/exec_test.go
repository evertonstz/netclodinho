package docker

import (
	"context"
	"strings"
	"testing"

	dockerclient "github.com/moby/moby/client"

	"github.com/angristan/netclode/services/control-plane/internal/config"
)

// execTestRuntime constructs a live daemon-backed Runtime for the exec integration
// tests, applying the Phase-2 daemon-gated + image-gated skip convention
// (runtime_test.go:42-56, :206-208): skip cleanly when no daemon is reachable and
// skip when the agent image is not present locally. It registers cleanup that
// force-removes the container and volume for the given sessionID.
func execTestRuntime(t *testing.T, sessionID string) *Runtime {
	t.Helper()

	const network = "netclode_default"
	const image = "netclode-agent:local"

	cfg := &config.Config{
		RuntimeMode:   config.RuntimeModeDocker,
		DockerNetwork: network,
		AgentImage:    image,
	}

	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Skipf("docker daemon not reachable, skipping: %v", err)
	}
	t.Cleanup(rt.Close)

	if _, inspectErr := rt.client.ImageInspect(context.Background(), image); inspectErr != nil {
		t.Skipf("agent image %q not present locally; skipping integration test: %v", image, inspectErr)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = rt.client.ContainerRemove(ctx, containerName(sessionID), dockerclient.ContainerRemoveOptions{Force: true})
		_, _ = rt.client.VolumeRemove(ctx, volumeName(sessionID), dockerclient.VolumeRemoveOptions{Force: true})
	})

	return rt
}

// TestExec_SplitStreamsAndExitCode verifies TERM-02: a one-shot exec against a live
// sandbox returns stdout and stderr as separately-demuxed strings and the correct
// non-zero exit code. The command writes to both streams and exits 3:
//
//	sh -c 'echo out; echo err >&2; exit 3'
//
// Assertions: Stdout contains "out", Stderr contains "err", ExitCode == 3, and the
// stdout buffer does NOT contain the stderr text — proving stdcopy demux truly split
// the multiplexed non-TTY stream (a raw io.Copy would smear both into one buffer with
// 8-byte frame headers).
//
// Daemon+image gated (skips cleanly without either). MUST FAIL against the current
// ErrNotSupported Exec stub (RED).
func TestExec_SplitStreamsAndExitCode(t *testing.T) {
	sessionID := "test-exec-split"
	rt := execTestRuntime(t, sessionID)

	if err := rt.CreateSandbox(context.Background(), sessionID, map[string]string{}, nil); err != nil {
		t.Fatalf("CreateSandbox() setup failed: %v", err)
	}

	res, err := rt.Exec(context.Background(), sessionID, "sh", "-c", "echo out; echo err >&2; exit 3")
	if err != nil {
		t.Fatalf("Exec() returned unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("Exec() returned nil result")
	}

	if !strings.Contains(res.Stdout, "out") {
		t.Errorf("Exec() Stdout = %q, want it to contain %q", res.Stdout, "out")
	}
	if !strings.Contains(res.Stderr, "err") {
		t.Errorf("Exec() Stderr = %q, want it to contain %q", res.Stderr, "err")
	}
	if res.ExitCode != 3 {
		t.Errorf("Exec() ExitCode = %d, want 3", res.ExitCode)
	}
	// Streams truly split: stderr text must NOT leak into stdout (demux worked).
	if strings.Contains(res.Stdout, "err") {
		t.Errorf("Exec() Stdout = %q leaked stderr text %q — stdcopy demux failed (streams not split)", res.Stdout, "err")
	}
}

// TestExec_NoLeak verifies TERM-02's "no leaked FDs / no wedge" criterion: looping
// Exec many times must not leak the hijacked connection. A leaked or unclosed hijack
// would surface as an error or a hang on a later iteration.
//
// Daemon+image gated. MUST FAIL against the current stub (RED).
func TestExec_NoLeak(t *testing.T) {
	sessionID := "test-exec-noleak"
	rt := execTestRuntime(t, sessionID)

	if err := rt.CreateSandbox(context.Background(), sessionID, map[string]string{}, nil); err != nil {
		t.Fatalf("CreateSandbox() setup failed: %v", err)
	}

	const iterations = 20
	for i := 0; i < iterations; i++ {
		res, err := rt.Exec(context.Background(), sessionID, "echo", "hi")
		if err != nil {
			t.Fatalf("Exec() iteration %d returned error (possible FD leak / wedged hijack): %v", i, err)
		}
		if res == nil {
			t.Fatalf("Exec() iteration %d returned nil result", i)
		}
		if !strings.Contains(res.Stdout, "hi") {
			t.Errorf("Exec() iteration %d Stdout = %q, want it to contain %q", i, res.Stdout, "hi")
		}
		if res.ExitCode != 0 {
			t.Errorf("Exec() iteration %d ExitCode = %d, want 0", i, res.ExitCode)
		}
	}
}
