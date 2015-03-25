// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"flag"
)

func Run(environ []string) error {
	var work WorkParameters
	if err := work.ProcessArguments(flag.CommandLine, environ); err != nil {
		return err
	}

	if work.remove {
		return work.Remove()
	}

	if err := work.Chown(); err != nil {
		return err
	}

	return work.Exec()
}
