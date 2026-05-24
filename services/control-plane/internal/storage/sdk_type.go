package storage

import (
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
)

// sdkTypeMappings maps every known SDK_TYPE_* string to its proto enum.
// New proto values: add entry here — init() will panic if you don't.
var sdkTypeMappings = map[string]pb.SdkType{
	"SDK_TYPE_CLAUDE":   pb.SdkType_SDK_TYPE_CLAUDE,
	"SDK_TYPE_OPENCODE": pb.SdkType_SDK_TYPE_OPENCODE,
	"SDK_TYPE_COPILOT":  pb.SdkType_SDK_TYPE_COPILOT,
	"SDK_TYPE_CODEX":    pb.SdkType_SDK_TYPE_CODEX,
	"SDK_TYPE_PI":       pb.SdkType_SDK_TYPE_PI,
}

func init() {
	for name := range pb.SdkType_value {
		if name == "SDK_TYPE_UNSPECIFIED" {
			continue
		}
		if _, ok := sdkTypeMappings[name]; !ok {
			panic("sdkTypeMappings missing proto value: " + name)
		}
	}
}
