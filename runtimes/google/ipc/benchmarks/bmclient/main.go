// a simple command-line tool to run the benchmark client.
package main

import (
	"flag"

	"veyron.io/veyron/veyron2/rt"
)

var (
	server      = flag.String("server", "", "object name of the server to connect to")
	count       = flag.Int("count", 1, "number of RPCs to send")
	chunkCount  = flag.Int("chunk_count", 0, "number of stream chunks to send")
	payloadSize = flag.Int("payload_size", 32, "the size of the payload")
)

func main() {
	runtime, err := rt.New()
	if err != nil {
		panic(err)
	}
	defer runtime.Cleanup()

	// TODO(jhahn): Fix this.
	/*
		ctx := runtime.NewContext()
		if *chunkCount == 0 {
			benchmarks.CallEcho(ctx, *server, *count, *payloadSize, os.Stdout)
		} else {
			benchmarks.CallEchoStream(runtime, *server, *count, *chunkCount, *payloadSize, os.Stdout)
		}
	*/
}
