package benchmark

import (
	"fmt"
	"testing"
	"time"

	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles/static"
	"v.io/core/veyron/runtimes/google/ipc/stream/manager"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
)

// benchmarkVCDial measures VC creation time over the underlying VIF.
func benchmarkVCDial(b *testing.B, mode options.VCSecurityLevel) {
	stats := testutil.NewBenchStats(16)

	server := manager.InternalNew(naming.FixedRoutingID(0x5))
	client := manager.InternalNew(naming.FixedRoutingID(0xc))

	_, ep, err := server.Listen("tcp", "127.0.0.1:0", mode)
	if err != nil {
		b.Fatal(err)
	}

	// Warmup to create the underlying VIF.
	_, err = client.Dial(ep, mode)
	if err != nil {
		b.Fatal(err)
	}

	// Reset the timer to exclude any underlying setup time from measurement.
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StartTimer()
		start := time.Now()

		_, err := client.Dial(ep, mode)
		if err != nil {
			b.Fatal(err)
		}

		duration := time.Since(start)
		b.StopTimer()

		stats.Add(duration)

		client.ShutdownEndpoint(ep)
	}

	client.Shutdown()
	server.Shutdown()

	fmt.Println()
	fmt.Println(stats)
}
