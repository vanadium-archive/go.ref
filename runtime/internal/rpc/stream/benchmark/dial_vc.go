// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"

	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	"v.io/x/ref/test"
	"v.io/x/ref/test/benchmark"
	"v.io/x/ref/test/testutil"
)

// benchmarkDialVC measures VC creation time over the underlying VIF.
func benchmarkDialVC(b *testing.B, mode options.SecurityLevel) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	stats := benchmark.AddStats(b, 16)

	server := manager.InternalNew(ctx, naming.FixedRoutingID(0x5))
	client := manager.InternalNew(ctx, naming.FixedRoutingID(0xc))
	var (
		principal security.Principal
		blessings security.Blessings
	)
	if mode == securityDefault {
		principal = testutil.NewPrincipal("test")
		blessings = principal.BlessingStore().Default()
		ctx, _ = v23.WithPrincipal(ctx, principal)
	}

	_, ep, err := server.Listen(ctx, "tcp", "127.0.0.1:0", blessings)

	if err != nil {
		b.Fatal(err)
	}

	// Create one VC to prevent the underlying VIF from being closed.
	_, err = client.Dial(ctx, ep, vc.IdleTimeout{Duration: 0})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer() // Exclude setup time from measurement.

	for i := 0; i < b.N; i++ {
		b.StartTimer()
		start := time.Now()

		VC, err := client.Dial(ctx, ep)
		if err != nil {
			b.Fatal(err)
		}

		duration := time.Since(start)
		b.StopTimer()

		stats.Add(duration)

		VC.Close(nil)
	}

	client.Shutdown()
	server.Shutdown()
}
