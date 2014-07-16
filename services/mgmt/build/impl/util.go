package impl

import (
	"runtime"

	"veyron2/services/mgmt/build"
)

func getArch() build.Architecture {
	switch runtime.GOARCH {
	case "386":
		return build.X86
	case "amd64":
		return build.AMD64
	case "arm":
		return build.ARM
	default:
		return build.UnsupportedArchitecture
	}
}

func getOS() build.OperatingSystem {
	switch runtime.GOOS {
	case "darwin":
		return build.Darwin
	case "linux":
		return build.Linux
	case "windows":
		return build.Windows
	default:
		return build.UnsupportedOperatingSystem
	}
}

func archString(arch build.Architecture) string {
	switch arch {
	case build.X86:
		return "x86"
	case build.AMD64:
		return "amd64"
	case build.ARM:
		return "arm"
	default:
		return "unsupported"
	}
}

func osString(os build.OperatingSystem) string {
	switch os {
	case build.Darwin:
		return "darwin"
	case build.Linux:
		return "linux"
	case build.Windows:
		return "windows"
	default:
		return "unsupported"
	}
}
