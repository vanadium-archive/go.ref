package buildinfo

import (
	"encoding/json"
	"runtime"
)

// vanadiumBuildInfo is filled in at link time, using:
//  -ldflags "-X v.io/core/veyron/lib/flags/buildinfo.vanadiumBuildInfo <value>"
var vanadiumBuildInfo string

// VanadiumBinaryInfo describes binary metadata.
type VanadiumBinaryInfo struct {
	GoVersion, BuildInfo string
}

// BinaryInfo returns the binary metadata as a JSON-encoded string, under the
// expectation that clients may want to parse it for specific bits of metadata.
func BinaryInfo() string {
	info := VanadiumBinaryInfo{
		GoVersion: runtime.Version(),
		BuildInfo: vanadiumBuildInfo,
	}
	jsonInfo, err := json.Marshal(info)
	if err != nil {
		return ""
	}
	return string(jsonInfo)
}
