package benchmark

import (
	"net"
	"testing"
	"time"

	"v.io/x/ref/profiles/internal/rpc/stream/vif"
	"v.io/x/ref/test/benchmark"
	"v.io/x/ref/test/testutil"

	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"
)

// benchmarkDialVIF measures VIF creation time over the underlying net connection.
func benchmarkDialVIF(b *testing.B, mode options.SecurityLevel) {
	stats := benchmark.AddStats(b, 16)
	var (
		principal security.Principal
		blessings security.Blessings
	)
	if mode == securityTLS {
		principal = testutil.NewPrincipal("test")
		blessings = principal.BlessingStore().Default()
	}

	b.ResetTimer() // Exclude setup time from measurement.

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		nc, ns := net.Pipe()

		server, err := vif.InternalNewAcceptedVIF(ns, naming.FixedRoutingID(0x5), principal, blessings, nil)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		start := time.Now()

		client, err := vif.InternalNewDialedVIF(nc, naming.FixedRoutingID(0xc), principal, nil)
		if err != nil {
			b.Fatal(err)
		}

		duration := time.Since(start)
		b.StopTimer()

		stats.Add(duration)

		client.Close()
		server.Close()
	}
}
