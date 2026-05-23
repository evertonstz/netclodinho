package controlplane

import (
	"strings"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
)

// sdkTypeLowerMappings maps every lowercase SDK type string to its proto enum.
// New proto values: add entry here — init() will panic if you don't.
var sdkTypeLowerMappings = map[string]pb.SdkType{
	"claude":   pb.SdkType_SDK_TYPE_CLAUDE,
	"opencode": pb.SdkType_SDK_TYPE_OPENCODE,
	"copilot":  pb.SdkType_SDK_TYPE_COPILOT,
	"codex":    pb.SdkType_SDK_TYPE_CODEX,
	"pi":       pb.SdkType_SDK_TYPE_PI,
}

func init() {
	// Verify every proto enum value name maps to something.
	// pb.SdkType_value is map[string]int32 — name → number.
	for name := range pb.SdkType_value {
		if name == "SDK_TYPE_UNSPECIFIED" {
			continue
		}
		lower := strings.ToLower(name)
		key := strings.TrimPrefix(lower, "sdk_type_")
		if _, ok := sdkTypeLowerMappings[key]; !ok {
			panic("sdkTypeLowerMappings missing proto value: " + name + " (key=" + key + ")")
		}
	}
}
