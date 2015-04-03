// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"runtime"

	"v.io/v23/services/build"
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
