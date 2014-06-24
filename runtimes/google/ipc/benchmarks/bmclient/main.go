// a simple command-line tool to run the benchmark client.
package main

import (
	"flag"
	"os"

	"veyron/runtimes/google/ipc/benchmarks"

	"veyron2/rt"
)

var (
	server      = flag.String("server", "", "veyron name of the server to connect to")
	count       = flag.Int("count", 1, "number of RPCs to send")
	chunkCount  = flag.Int("chunk_count", 0, "number of stream chunks to send")
	payloadSize = flag.Int("payload_size", 32, "the size of the payload")
)

func main() {
	rt.Init()
	if *chunkCount == 0 {
		benchmarks.CallEcho(*server, *count, *payloadSize, os.Stdout)
	} else {
		benchmarks.CallEchoStream(*server, *count, *chunkCount, *payloadSize, os.Stdout)
	}
}
