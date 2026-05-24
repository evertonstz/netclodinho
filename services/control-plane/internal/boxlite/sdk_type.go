package boxlite

import (
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
)

// sdkTypeStringMappings maps every known SDK_TYPE_* string to its proto enum.
// New proto values: add entry here — init() will panic if you don't.
var sdkTypeStringMappings = map[string]pb.SdkType{
	"SDK_TYPE_CLAUDE":   pb.SdkType_SDK_TYPE_CLAUDE,
	"SDK_TYPE_OPENCODE": pb.SdkType_SDK_TYPE_OPENCODE,
	"SDK_TYPE_COPILOT":  pb.SdkType_SDK_TYPE_COPILOT,
	"SDK_TYPE_CODEX":    pb.SdkType_SDK_TYPE_CODEX,
	"SDK_TYPE_PI":       pb.SdkType_SDK_TYPE_PI,
}

func init() {
	// Verify every proto enum value name is in our map.
	// pb.SdkType_value is map[string]int32 — name → number.
	// Missing in our map → panic at startup → caught by tests/CI.
	for name := range pb.SdkType_value {
		if name == "SDK_TYPE_UNSPECIFIED" {
			continue
		}
		if _, ok := sdkTypeStringMappings[name]; !ok {
			panic("sdkTypeStringMappings missing proto value: " + name)
		}
	}
}

// sdkTypeFromString maps a proto enum name string (e.g. "SDK_TYPE_PI").
// Falls back to SDK_TYPE_CLAUDE for unknown strings.
func sdkTypeFromString(name string) pb.SdkType {
	if v, ok := sdkTypeStringMappings[name]; ok {
		return v
	}
	return pb.SdkType_SDK_TYPE_CLAUDE
}
