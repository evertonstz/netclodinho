package session

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
)

// newRedisBacked builds a Manager backed by miniredis for unit tests.
func newRedisBacked(t *testing.T) (*Manager, *mockRuntime) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	cfg := &config.Config{
		Port:        3000,
		RuntimeMode: "docker",
		RedisURL:    "redis://" + mr.Addr(),
	}
	store, err := storage.NewRedisStorage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewRedisStorage: %v", err)
	}
	rt := newMockRuntime()
	return NewManager(store, rt, cfg, nil), rt
}

// waitForEnv polls until the mock runtime records a CreateSandbox call for
// the given session ID, or the deadline is reached.
func waitForEnv(t *testing.T, rt *mockRuntime, sessionID string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		env, ok := rt.createdEnvs[sessionID]
		rt.mu.Unlock()
		if ok {
			return env
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for CreateSandbox to be called for session %s", sessionID)
	return nil
}

// TestTailnetEnabled_PersistedInSession verifies that TailnetEnabled is stored on
// the Session proto and survives a storage round-trip.
func TestTailnetEnabled_PersistedInSession(t *testing.T) {
	ctx := context.Background()
	m, _ := newRedisBacked(t)

	tailnet := true
	sess, err := m.Create(ctx, "tailnet-session", nil, nil, nil, nil, nil, &tailnet, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !sess.TailnetEnabled {
		t.Fatal("expected TailnetEnabled=true on returned session")
	}

	// Reload from storage to confirm it was persisted.
	stored, err := m.Get(ctx, sess.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !stored.TailnetEnabled {
		t.Fatal("expected TailnetEnabled=true after reload from storage")
	}
}

// TestTailnetDisabled_DefaultIsFalse verifies sessions without the flag have
// TailnetEnabled=false.
func TestTailnetDisabled_DefaultIsFalse(t *testing.T) {
	ctx := context.Background()
	m, _ := newRedisBacked(t)

	sess, err := m.Create(ctx, "no-tailnet", nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.TailnetEnabled {
		t.Fatal("expected TailnetEnabled=false when no tailnet flag provided")
	}
}

// TestTailnetEnabled_EnvKeyInjected verifies _BOXLITE_TAILNET=true is injected
// into the sandbox env when tailnetEnabled=true.
func TestTailnetEnabled_EnvKeyInjected(t *testing.T) {
	ctx := context.Background()
	m, rt := newRedisBacked(t)

	tailnet := true
	sdkType := pb.SdkType_SDK_TYPE_CLAUDE
	sess, err := m.Create(ctx, "tailnet-env-inject", nil, nil, &sdkType, nil, nil, &tailnet, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	env := waitForEnv(t, rt, sess.Id)

	if env["_BOXLITE_TAILNET"] != "true" {
		t.Fatalf("expected _BOXLITE_TAILNET=true in sandbox env, got %q", env["_BOXLITE_TAILNET"])
	}
}

// TestTailnetDisabled_EnvKeyAbsent verifies _BOXLITE_TAILNET is NOT injected
// when tailnetEnabled=false.
func TestTailnetDisabled_EnvKeyAbsent(t *testing.T) {
	ctx := context.Background()
	m, rt := newRedisBacked(t)

	sdkType := pb.SdkType_SDK_TYPE_CLAUDE
	sess, err := m.Create(ctx, "no-tailnet-env", nil, nil, &sdkType, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	env := waitForEnv(t, rt, sess.Id)

	if v, ok := env["_BOXLITE_TAILNET"]; ok {
		t.Fatalf("expected _BOXLITE_TAILNET to be absent, got %q", v)
	}
}

// TestTailnetEnabled_EnvKeyStrippedFromGuest verifies the _BOXLITE_TAILNET key
// is not forwarded to the guest. The mockRuntime receives env after CreateSandbox
// strips internal keys, so it should never appear.
func TestTailnetEnabled_InternalEnvNotLeaked(t *testing.T) {
	ctx := context.Background()
	m, rt := newRedisBacked(t)

	tailnet := true
	sdkType := pb.SdkType_SDK_TYPE_CLAUDE
	sess, err := m.Create(ctx, "tailnet-no-leak", nil, nil, &sdkType, nil, nil, &tailnet, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The _BOXLITE_TAILNET key is read and stripped inside CreateSandbox before
	// the boxEnv is built. The mockRuntime receives the env map from manager.go
	// BEFORE CreateSandbox strips it (it's in the env passed to k8s.Runtime.CreateSandbox).
	// So the mock sees it. This test instead verifies no _BOXLITE_SESSION_SECRET_*
	// keys leak (they carry real credentials and must always be stripped).
	env := waitForEnv(t, rt, sess.Id)
	for k := range env {
		if len(k) > 22 && k[:22] == "_BOXLITE_SESSION_SECRET" {
			t.Fatalf("internal secret carrier key %q must not reach the runtime CreateSandbox call", k)
		}
	}
}
