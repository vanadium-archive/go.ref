// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package syncbased_vine implements syncbased, the Syncbase daemon, with a
// VINE server to enable test-specific network configuration.
package syncbased_vine

import (
	"v.io/v23"
	"v.io/v23/security"
	"v.io/x/lib/gosh"
	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/runtime/protocols/vine"
	"v.io/x/ref/services/syncbase/syncbaselib"
)

func Main(vineServerName string, vineTag string, opts syncbaselib.Opts) {
	ctx, shutdown := v23.Init()
	defer shutdown()

	// Start a VINE Server and modify our ctx to use the "vine" protocol.
	// The final argument is the discoveryTTL, which uses a sensible default if
	// called with 0.
	ctx, err := vine.Init(ctx, vineServerName, security.AllowEveryone(), vineTag, 0)
	if err != nil {
		panic(err)
	}

	// Syncbase now uses a modified rpc.ListenSpec which has been set to use
	// the VINE protocol.
	s, _, cleanup := syncbaselib.Serve(ctx, opts)
	if eps := s.Status().Endpoints; len(eps) == 0 {
		panic("s.Status().Endpoints is empty")
	}
	gosh.SendVars(map[string]string{
		"ENDPOINT": s.Status().Endpoints[0].String(),
	})
	defer cleanup()
	ctx.Info("Received signal ", <-signals.ShutdownOnSignals(ctx))
}
