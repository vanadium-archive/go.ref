// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package logger_test

import (
	"testing"

	"v.io/x/lib/vlog"

	"v.io/v23/logging"

	"v.io/x/ref/internal/logger"
)

func TestManager(t *testing.T) {
	global := logger.Global()
	if _, ok := global.(*vlog.Logger); !ok {
		t.Fatalf("global logger is not a vlog.Logger")
	}

	manager := logger.Manager(logger.Global())
	if _, ok := manager.(*vlog.Logger); !ok {
		t.Fatalf("logger.Manager does not return a vlog.Logger")
	}

	// Make sure vlog.Log satisfies the logging interfaces
	var _ logger.ManageLog = vlog.Log
	var _ logging.Logger = vlog.Log
}
