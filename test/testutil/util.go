// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DepthToExternalCaller determines the number of stack frames to the first
// enclosing caller that is external to the package that this function is
// called from. Drectory name is used as a proxy for package name,
// that is, the directory component of the file return runtime.Caller is
// compared to that of the lowest level caller until a different one is
// encountered as the stack is walked upwards.
func DepthToExternalCaller() int {
	_, file, _, _ := runtime.Caller(1)
	cwd := filepath.Dir(file)
	for d := 2; d < 10; d++ {
		_, file, _, _ := runtime.Caller(d)
		if cwd != filepath.Dir(file) {
			return d
		}
	}
	return 1
}

// FormatLogLine will prepend the file and line number of the caller
// at the specificied depth (as per runtime.Caller) to the supplied
// format and args and return a formatted string. It is useful when
// implementing functions that factor out error handling and reporting
// in tests.
func FormatLogLine(depth int, format string, args ...interface{}) string {
	_, file, line, _ := runtime.Caller(depth)
	nargs := []interface{}{filepath.Base(file), line}
	nargs = append(nargs, args...)
	return fmt.Sprintf("%s:%d: "+format, nargs...)
}

// UnsetPrincipalEnvVars unsets all environment variables pertaining to
// principal initialization.
func UnsetPrincipalEnvVars() {
	os.Setenv("VEYRON_CREDENTIALS", "")
	os.Setenv("VEYRON_AGENT_FD", "")
}
