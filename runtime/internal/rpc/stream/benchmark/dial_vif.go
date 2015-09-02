// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"net"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"

	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/vif"
	"v.io/x/ref/test"
	"v.io/x/ref/test/benchmark"
	"v.io/x/ref/test/testutil"
)

// benchmarkDialVIF measures VIF creation time over the underlying net connection.
func benchmarkDialVIF(b *testing.B, mode options.SecurityLevel) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	stats := benchmark.AddStats(b, 16)
	var (
		principal     security.Principal
		blessings     security.Blessings
		authenticated bool
	)
	if mode == securityDefault {
		principal = testutil.NewPrincipal("test")
		blessings = principal.BlessingStore().Default()
		ctx, _ = v23.WithPrincipal(ctx, principal)
		authenticated = true
	}

	b.ResetTimer() // Exclude setup time from measurement.

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		nc, ns := net.Pipe()

		serverch := make(chan *vif.VIF)
		go func() {
			server, err := vif.InternalNewAcceptedVIF(ctx, ns, naming.FixedRoutingID(0x5), blessings, nil, nil, stream.AuthenticatedVC(authenticated))
			if err != nil {
				panic(err)
			}
			serverch <- server
		}()

		b.StartTimer()
		start := time.Now()

		client, err := vif.InternalNewDialedVIF(ctx, nc, naming.FixedRoutingID(0xc), nil, nil, stream.AuthenticatedVC(authenticated))
		if err != nil {
			b.Fatal(err)
		}

		duration := time.Since(start)
		b.StopTimer()

		stats.Add(duration)

		client.Close()
		if server := <-serverch; server != nil {
			server.Close()
		}
	}
}
