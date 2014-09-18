package impl

import (
	"runtime"

	"veyron.io/veyron/veyron2/services/mgmt/build"
)

func getArch() build.Architecture {
	switch runtime.GOARCH {
	case "386", "amd64", "arm":
		return build.Architecture(runtime.GOARCH)
	default:
		return build.UnsupportedArchitecture
	}
}

func getOS() build.OperatingSystem {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return build.OperatingSystem(runtime.GOOS)
	default:
		return build.UnsupportedOS
	}
}
