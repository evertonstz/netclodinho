package controlplane

import (
	"testing"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
)

func TestParseSdkType(t *testing.T) {
	tests := []struct {
		input string
		want  pb.SdkType
	}{
		{"claude", pb.SdkType_SDK_TYPE_CLAUDE},
		{"Claude", pb.SdkType_SDK_TYPE_CLAUDE},
		{"CLAUDE", pb.SdkType_SDK_TYPE_CLAUDE},
		{"opencode", pb.SdkType_SDK_TYPE_OPENCODE},
		{"copilot", pb.SdkType_SDK_TYPE_COPILOT},
		{"codex", pb.SdkType_SDK_TYPE_CODEX},
		{"unknown", pb.SdkType_SDK_TYPE_CLAUDE},
		{"", pb.SdkType_SDK_TYPE_CLAUDE},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseSdkType(tt.input); got != tt.want {
				t.Errorf("ParseSdkType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
