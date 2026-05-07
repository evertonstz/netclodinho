package boxlite

import (
	"testing"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
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

	want := map[string]string{
		"github_copilot_oauth_access":  "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
		"github_copilot_oauth_refresh": "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
	}
	found := map[string]bool{}
	for _, binding := range bindings {
		placeholder, ok := want[binding.Name]
		if !ok {
			continue
		}
		found[binding.Name] = true
		if binding.Placeholder != placeholder {
			t.Fatalf("unexpected placeholder for %s: %q", binding.Name, binding.Placeholder)
		}
	}
	for name := range want {
		if !found[name] {
			t.Fatalf("expected %s binding", name)
		}
	}

	for _, expected := range []string{"api.github.com", "api.githubcopilot.com", "api.individual.githubcopilot.com", "copilot-proxy.githubusercontent.com"} {
		if !contains(allowNet, expected) {
			t.Fatalf("expected allowNet to include %q, got %v", expected, allowNet)
		}
	}
}

func TestShouldExposeGuestPlaceholderEnv(t *testing.T) {
	if shouldExposeGuestPlaceholderEnv("github_copilot_oauth_access") {
		t.Fatal("github_copilot_oauth_access should not be exposed as a guest env placeholder")
	}
	if shouldExposeGuestPlaceholderEnv("github_copilot_oauth_refresh") {
		t.Fatal("github_copilot_oauth_refresh should not be exposed as a guest env placeholder")
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

func contains(slice []string, want string) bool {
	for _, item := range slice {
		if item == want {
			return true
		}
	}
	return false
}

type stubTokenIssuer struct{}

func (stubTokenIssuer) IssueDockerToken(sessionID string) string {
	return "token-for-" + sessionID
}

func TestBuildSandboxCreateSpecAppliesPersistedResources(t *testing.T) {
	r := &Runtime{
		cfg: &config.Config{
			AgentImage:               "ghcr.io/angristan/netclode-agent:latest",
			BoxliteAgentCPURL:        "https://cp.example.com",
			BoxliteDefaultDiskSizeGb: 20,
		},
		realKeys:    map[string]string{"anthropic": "sk-real"},
		tokenIssuer: stubTokenIssuer{},
	}

	spec := r.buildSandboxCreateSpec("sess-123", map[string]string{
		"SDK_TYPE":                  pb.SdkType_SDK_TYPE_CLAUDE.String(),
		"FOO":                       "bar",
		k8s.ExistingPVCEnvKey:       "persisted-box",
		"_BOXLITE_SESSION_SECRET_x": "secret-value",
	}, &k8s.SandboxResourceConfig{VCPUs: 6, MemoryMB: 12288, DiskSizeGb: 80})

	if spec.existingBoxName != "persisted-box" {
		t.Fatalf("existingBoxName = %q, want persisted-box", spec.existingBoxName)
	}
	if spec.diskSizeGb != 80 {
		t.Fatalf("diskSizeGb = %d, want 80", spec.diskSizeGb)
	}
	if spec.cpus != 6 {
		t.Fatalf("cpus = %d, want 6", spec.cpus)
	}
	if spec.memoryMB != 12288 {
		t.Fatalf("memoryMB = %d, want 12288", spec.memoryMB)
	}
	if spec.boxEnv["AGENT_SESSION_TOKEN"] != "token-for-sess-123" {
		t.Fatalf("AGENT_SESSION_TOKEN = %q", spec.boxEnv["AGENT_SESSION_TOKEN"])
	}
	if spec.boxEnv["CONTROL_PLANE_URL"] != "https://cp.example.com" {
		t.Fatalf("CONTROL_PLANE_URL = %q", spec.boxEnv["CONTROL_PLANE_URL"])
	}
	if spec.boxEnv["ANTHROPIC_API_KEY"] != "NETCLODE_PLACEHOLDER_anthropic" {
		t.Fatalf("ANTHROPIC_API_KEY placeholder = %q", spec.boxEnv["ANTHROPIC_API_KEY"])
	}
	if _, ok := spec.boxEnv[k8s.ExistingPVCEnvKey]; ok {
		t.Fatal("existing PVC env key should not be forwarded to guest env")
	}
	if _, ok := spec.boxEnv["_BOXLITE_SESSION_SECRET_x"]; ok {
		t.Fatal("per-session secret carrier should not be forwarded to guest env")
	}
}

func TestBuildSandboxCreateSpecUsesDefaultDiskWithoutResources(t *testing.T) {
	r := &Runtime{
		cfg:         &config.Config{BoxliteAgentCPURL: "https://cp.example.com", BoxliteDefaultDiskSizeGb: 24},
		realKeys:    map[string]string{},
		tokenIssuer: stubTokenIssuer{},
	}

	spec := r.buildSandboxCreateSpec("sess-456", map[string]string{"SDK_TYPE": pb.SdkType_SDK_TYPE_CLAUDE.String()}, nil)
	if spec.diskSizeGb != 24 {
		t.Fatalf("diskSizeGb = %d, want 24", spec.diskSizeGb)
	}
	if spec.cpus != 0 || spec.memoryMB != 0 {
		t.Fatalf("unexpected default resource overrides: cpus=%d memoryMB=%d", spec.cpus, spec.memoryMB)
	}
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
