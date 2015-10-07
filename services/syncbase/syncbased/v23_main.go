// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !mojo

// syncbased is a syncbase daemon.
package main

// Example invocation:
// syncbased --veyron.tcp.address="127.0.0.1:0" --name=syncbased

import (
	"v.io/v23"
	"v.io/x/ref/lib/signals"
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()
	_, _, cleanup := Serve(ctx)
	defer cleanup()
	<-signals.ShutdownOnSignals(ctx)
}
