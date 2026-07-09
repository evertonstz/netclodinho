package docker

import (
	"context"
	"strings"
	"testing"
	"time"

	dockerclient "github.com/moby/moby/client"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
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

// TestContainerVolumeName asserts that containerName and volumeName both return
// "netclode-<sessionID>" for a given input. No daemon required.
func TestContainerVolumeName(t *testing.T) {
	sessionID := "abc"
	want := "netclode-abc"

	if got := containerName(sessionID); got != want {
		t.Errorf("containerName(%q) = %q, want %q", sessionID, got, want)
	}
	if got := volumeName(sessionID); got != want {
		t.Errorf("volumeName(%q) = %q, want %q", sessionID, got, want)
	}
}

// TestGetPVCName asserts that GetPVCName returns "netclode-<sessionID>", nil.
// Constructs a Runtime literal directly to avoid the Ping in NewRuntime.
func TestGetPVCName(t *testing.T) {
	rt := &Runtime{
		cfg:            &config.Config{DockerNetwork: "netclode_default"},
		readyChannels:  make(map[string][]chan struct{}),
		containerCache: make(map[string]string),
	}

	name, err := rt.GetPVCName(context.Background(), "abc")
	if err != nil {
		t.Fatalf("GetPVCName() returned error: %v", err)
	}
	if name != "netclode-abc" {
		t.Errorf("GetPVCName() = %q, want %q", name, "netclode-abc")
	}
}

// TestReadyChannelNotify verifies that WatchSandboxReady + NotifyAgentReady fire
// the callback with the expected FQDN. No daemon required — uses a Runtime literal.
func TestReadyChannelNotify(t *testing.T) {
	rt := &Runtime{
		cfg:            &config.Config{DockerNetwork: "netclode_default"},
		readyChannels:  make(map[string][]chan struct{}),
		containerCache: make(map[string]string),
	}

	sessionID := "session123"
	wantFQDN := "netclode-" + sessionID

	type callbackResult struct {
		id   string
		fqdn string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	rt.WatchSandboxReady(sessionID, func(id, fqdn string, err error) {
		resultCh <- callbackResult{id: id, fqdn: fqdn, err: err}
	})

	rt.NotifyAgentReady(sessionID)

	select {
	case got := <-resultCh:
		if got.id != sessionID {
			t.Errorf("callback id = %q, want %q", got.id, sessionID)
		}
		if got.fqdn != wantFQDN {
			t.Errorf("callback fqdn = %q, want %q", got.fqdn, wantFQDN)
		}
		if got.err != nil {
			t.Errorf("callback err = %v, want nil", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback was not invoked within 2 seconds after NotifyAgentReady")
	}
}

// TestWaitForReady_Timeout verifies that WaitForReady returns an error containing
// "timed out" when no NotifyAgentReady arrives before the deadline.
// No daemon required.
func TestWaitForReady_Timeout(t *testing.T) {
	rt := &Runtime{
		cfg:            &config.Config{DockerNetwork: "netclode_default"},
		readyChannels:  make(map[string][]chan struct{}),
		containerCache: make(map[string]string),
	}

	ctx := context.Background()
	_, err := rt.WaitForReady(ctx, "nosuchsession", 50*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForReady() should return an error on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("WaitForReady() error = %q, want it to contain \"timed out\"", err.Error())
	}
}

// TestCreateSandbox verifies CreateSandbox end-to-end: creates the container with the
// agent image, workspace volume, labels, and network attachment (SESS-01, DEVX-03).
//
// The test is daemon-gated: if NewRuntime fails (no daemon), it skips cleanly.
// When a daemon IS present but the required network/image are absent, it also skips.
//
// t.Cleanup force-removes any residue to avoid name conflicts on re-run (Pitfall 5).
func TestCreateSandbox(t *testing.T) {
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
	defer rt.Close()

	// Skip if the agent image is not present locally (DEVX-03: no pull in tests).
	if _, inspectErr := rt.client.ImageInspect(context.Background(), image); inspectErr != nil {
		t.Skipf("agent image %q not present locally; skipping integration test: %v", image, inspectErr)
	}

	sessionID := "test-createsandbox"

	// Register cleanup BEFORE creating the sandbox to ensure cleanup runs even on failure.
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = rt.client.ContainerRemove(ctx, containerName(sessionID), dockerclient.ContainerRemoveOptions{Force: true})
		_, _ = rt.client.VolumeRemove(ctx, volumeName(sessionID), dockerclient.VolumeRemoveOptions{Force: true})
	})

	env := map[string]string{
		"SESSION_ID": sessionID,
	}

	if err := rt.CreateSandbox(context.Background(), sessionID, env, nil); err != nil {
		t.Fatalf("CreateSandbox() returned unexpected error: %v", err)
	}

	// Verify the container exists and is running.
	inspectResult, err := rt.client.ContainerInspect(context.Background(), containerName(sessionID), dockerclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("ContainerInspect() after CreateSandbox failed: %v", err)
	}
	if inspectResult.Container.State == nil || !inspectResult.Container.State.Running {
		t.Errorf("container %q should be running after CreateSandbox, got state: %+v", containerName(sessionID), inspectResult.Container.State)
	}

	// Verify the workspace volume is mounted at /agent.
	foundMount := false
	for _, m := range inspectResult.Container.Mounts {
		if m.Destination == "/agent" && m.Name == volumeName(sessionID) {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Errorf("container should have volume %q mounted at /agent; mounts: %+v", volumeName(sessionID), inspectResult.Container.Mounts)
	}

	// Verify the container is attached to the compose network.
	if _, ok := inspectResult.Container.NetworkSettings.Networks[network]; !ok {
		t.Errorf("container should be on network %q; networks: %v", network, inspectResult.Container.NetworkSettings.Networks)
	}
}

// TestCreateSandboxResume verifies the resume path: when ExistingPVCEnvKey is set,
// CreateSandbox starts an existing stopped container without creating a new one.
//
// Daemon-gated; skips cleanly when no daemon or required image/network absent.
func TestCreateSandboxResume(t *testing.T) {
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
	defer rt.Close()

	if _, inspectErr := rt.client.ImageInspect(context.Background(), image); inspectErr != nil {
		t.Skipf("agent image %q not present locally; skipping integration test: %v", image, inspectErr)
	}

	sessionID := "test-resumesandbox"

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = rt.client.ContainerRemove(ctx, containerName(sessionID), dockerclient.ContainerRemoveOptions{Force: true})
		_, _ = rt.client.VolumeRemove(ctx, volumeName(sessionID), dockerclient.VolumeRemoveOptions{Force: true})
	})

	// First create a sandbox normally so the container exists.
	if err := rt.CreateSandbox(context.Background(), sessionID, map[string]string{}, nil); err != nil {
		t.Fatalf("initial CreateSandbox() failed: %v", err)
	}

	// Stop the container to simulate a paused session.
	if _, err := rt.client.ContainerStop(context.Background(), containerName(sessionID), dockerclient.ContainerStopOptions{}); err != nil {
		t.Fatalf("ContainerStop() setup failed: %v", err)
	}

	// Resume: pass ExistingPVCEnvKey pointing to the existing container name.
	resumeEnv := map[string]string{
		k8s.ExistingPVCEnvKey: containerName(sessionID),
	}
	if err := rt.CreateSandbox(context.Background(), sessionID, resumeEnv, nil); err != nil {
		t.Fatalf("CreateSandbox(resume) returned unexpected error: %v", err)
	}

	// Verify the container is running again.
	inspectResult, err := rt.client.ContainerInspect(context.Background(), containerName(sessionID), dockerclient.ContainerInspectOptions{})
	if err != nil {
		t.Fatalf("ContainerInspect() after resume failed: %v", err)
	}
	if inspectResult.Container.State == nil || !inspectResult.Container.State.Running {
		t.Errorf("container should be running after resume, got state: %+v", inspectResult.Container.State)
	}
}

// TestImageLocalSkipsPull verifies that ensureImage returns nil immediately when
// the image is already present locally (DEVX-03 inspect-then-skip path).
//
// The test is daemon-gated: if NewRuntime fails, the daemon is assumed unavailable
// and the test is skipped. When a daemon IS present, it inspects for an image that
// is guaranteed to exist on any Docker install (docker.io/library/hello-world or
// similar); if the image is absent it also skips, keeping the assertion deterministic.
//
// Manual DEVX-03 verification (see 02-VALIDATION.md "Manual-Only Verifications"):
//
//	docker build -t netclode-agent:local ./agent
//	AGENT_IMAGE=netclode-agent:local DOCKER_NETWORK=netclode_default \
//	  go test ./internal/docker/... -run TestImageLocalSkipsPull -v
func TestImageLocalSkipsPull(t *testing.T) {
	// Probe image: hello-world is a 13 KiB scratch image present in nearly every
	// Docker install. We avoid pulling it by skipping when inspect fails.
	probeImage := "hello-world:latest"

	cfg := &config.Config{
		RuntimeMode:   config.RuntimeModeDocker,
		DockerNetwork: "netclode_default",
		AgentImage:    probeImage,
	}

	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Skipf("docker daemon not reachable, skipping: %v", err)
	}
	defer rt.Close()

	// Check whether the probe image is present locally; skip if not.
	_, inspectErr := rt.client.ImageInspect(context.Background(), probeImage)
	if inspectErr != nil {
		t.Skipf("probe image %q not present locally; skipping DEVX-03 unit check: %v", probeImage, inspectErr)
	}

	// Image is confirmed local — ensureImage must return nil without pulling.
	if err := rt.ensureImage(context.Background()); err != nil {
		t.Errorf("ensureImage() returned error for locally-present image: %v", err)
	}
}
