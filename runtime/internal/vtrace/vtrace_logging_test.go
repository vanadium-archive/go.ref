// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vtrace_test

import (
	"strings"
	"testing"

	"v.io/v23/vtrace"
)

// TestLogging tests to make sure that ctx.Infof comments are added to the trace
func TestLogging(t *testing.T) {
	ctx, shutdown, _ := initForTest(t)
	defer shutdown()

	ctx, span := vtrace.WithNewTrace(ctx)
	vtrace.ForceCollect(ctx)
	ctx, span = vtrace.WithNewSpan(ctx, "foo")
	ctx.Info("logging ", "from ", "info")
	ctx.Infof("logging from %s", "infof")
	ctx.InfoDepth(0, "logging from info depth")

	ctx.Error("logging ", "from ", "error")
	ctx.Errorf("logging from %s", "errorf")
	ctx.ErrorDepth(0, "logging from error depth")

	span.Finish()
	record := vtrace.GetStore(ctx).TraceRecord(span.Trace())
	messages := []string{
		"vtrace_logging_test.go:22] logging from info",
		"vtrace_logging_test.go:23] logging from infof",
		"vtrace_logging_test.go:24] logging from info depth",
		"vtrace_logging_test.go:26] logging from error",
		"vtrace_logging_test.go:27] logging from errorf",
		"vtrace_logging_test.go:28] logging from error depth",
	}
	expectSequence(t, *record, []string{
		"foo: " + strings.Join(messages, ", "),
	})
}