package boxlite

import (
	"os"
	"path/filepath"
	"testing"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
)

func TestBuildSecretsAndAllowNet_Claude(t *testing.T) {
	bindings, allowNet := BuildSecretsAndAllowNet(pb.SdkType_SDK_TYPE_CLAUDE, map[string]string{"anthropic": "sk-ant-real"})
	if len(bindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(bindings))
	}
	if bindings[0].Name != "anthropic" || bindings[0].Value != "sk-ant-real" {
		t.Fatalf("unexpected binding: %+v", bindings[0])
	}
	if len(allowNet) != 1 || allowNet[0] != "api.anthropic.com" {
		t.Fatalf("unexpected allowNet: %v", allowNet)
	}
}

func TestBuildSecretsAndAllowNet_UsesPlaceholderWhenMissing(t *testing.T) {
	bindings, _ := BuildSecretsAndAllowNet(pb.SdkType_SDK_TYPE_CLAUDE, map[string]string{})
	if len(bindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(bindings))
	}
	if bindings[0].Value != "NETCLODE_PLACEHOLDER_anthropic" {
		t.Fatalf("want placeholder value, got %q", bindings[0].Value)
	}
}

func TestBuildSecretsAndAllowNet_OpencodeDedupesHosts(t *testing.T) {
	bindings, allowNet := BuildSecretsAndAllowNet(pb.SdkType_SDK_TYPE_OPENCODE, map[string]string{})
	if len(bindings) < 4 {
		t.Fatalf("expected multiple bindings, got %d", len(bindings))
	}
	seen := map[string]bool{}
	for _, host := range allowNet {
		if seen[host] {
			t.Fatalf("duplicate host in allowNet: %s", host)
		}
		seen[host] = true
	}
}

func TestBuildSecretsAndAllowNet_OpencodeIncludesCopilotOAuthHosts(t *testing.T) {
	bindings, allowNet := BuildSecretsAndAllowNet(pb.SdkType_SDK_TYPE_OPENCODE, map[string]string{})

	found := false
	for _, binding := range bindings {
		if binding.Name == "github_copilot_oauth" {
			found = true
			if binding.Placeholder != "NETCLODE_PLACEHOLDER_github_copilot_oauth" {
				t.Fatalf("unexpected placeholder: %q", binding.Placeholder)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected github_copilot_oauth binding")
	}

	for _, expected := range []string{"api.github.com", "api.githubcopilot.com", "api.individual.githubcopilot.com", "copilot-proxy.githubusercontent.com"} {
		if !contains(allowNet, expected) {
			t.Fatalf("expected allowNet to include %q, got %v", expected, allowNet)
		}
	}
}

func TestShouldExposeGuestPlaceholderEnv(t *testing.T) {
	if shouldExposeGuestPlaceholderEnv("github_copilot_oauth") {
		t.Fatal("github_copilot_oauth should not be exposed as a guest env placeholder")
	}
	if !shouldExposeGuestPlaceholderEnv("anthropic") {
		t.Fatal("anthropic should be exposed as a guest env placeholder")
	}
}

func TestExtractHost(t *testing.T) {
	if got := extractHost("http://example.com:8080/path"); got != "example.com" {
		t.Fatalf("want example.com, got %q", got)
	}
	if got := extractHost("example.com"); got != "example.com" {
		t.Fatalf("want example.com, got %q", got)
	}
}

func TestAppendUnique(t *testing.T) {
	got := appendUnique([]string{"a", "b"}, "b")
	if len(got) != 2 {
		t.Fatalf("expected no duplicate, got %v", got)
	}
	got = appendUnique(got, "c")
	if len(got) != 3 || got[2] != "c" {
		t.Fatalf("unexpected append result: %v", got)
	}
}

func TestAllowedWorkspaceRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !isAllowedWorkspaceRoot(filepath.Join(home, ".boxlite", "test")) {
		t.Fatal("expected ~/.boxlite path to be allowed")
	}
	if !isAllowedWorkspaceRoot("/var/lib/netclode/workspaces/test") {
		t.Fatal("expected /var/lib/netclode path to be allowed")
	}
	if isAllowedWorkspaceRoot("/tmp/not-allowed") {
		t.Fatal("unexpectedly allowed /tmp path")
	}
}

func contains(slice []string, want string) bool {
	for _, item := range slice {
		if item == want {
			return true
		}
	}
	return false
}

func TestNewRuntimeCreatesHomeDir(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{AgentImage: "alpine:latest", BoxliteHomeDir: home}
	r, err := NewRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if r == nil || r.rt == nil {
		t.Fatal("runtime not initialized")
	}
	r.Close()
}
