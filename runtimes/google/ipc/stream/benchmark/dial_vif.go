package benchmark

import (
	"fmt"
	"net"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vif"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
)

// benchmarkVIFDial measures VIF creation time over the underlying net connection.
func benchmarkVIFDial(b *testing.B, mode options.VCSecurityLevel) {
	stats := testutil.NewBenchStats(16)

	// Reset the timer to exclude any underlying setup time from measurement.
	b.ResetTimer()

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

	fmt.Println()
	fmt.Println(stats)
}
