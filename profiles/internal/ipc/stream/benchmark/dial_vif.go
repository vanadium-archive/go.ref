package benchmark

import (
	"net"
	"testing"
	"time"

	"v.io/x/ref/lib/testutil/benchmark"
	"v.io/x/ref/profiles/internal/ipc/stream/vif"

	"v.io/v23/naming"
	"v.io/v23/options"
)

// benchmarkDialVIF measures VIF creation time over the underlying net connection.
func benchmarkDialVIF(b *testing.B, mode options.VCSecurityLevel) {
	stats := benchmark.AddStats(b, 16)

	b.ResetTimer() // Exclude setup time from measurement.

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		nc, ns := net.Pipe()

		server, err := vif.InternalNewAcceptedVIF(ns, naming.FixedRoutingID(0x5), nil, mode)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		start := time.Now()

		client, err := vif.InternalNewDialedVIF(nc, naming.FixedRoutingID(0xc), nil, mode)
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
