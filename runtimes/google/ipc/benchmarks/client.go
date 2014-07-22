package benchmarks

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"veyron2"
	"veyron2/rt"
	"veyron2/vlog"
)

// CallEcho calls the Echo method 'iterations' times with the given payload
// size, and optionally logs the result.
func CallEcho(address string, iterations, payloadSize int, log io.Writer) {
	payload := make([]byte, payloadSize)
	for _, i := range payload {
		payload[i] = byte(i & 0xff)
	}

	stub, err := BindBenchmark(address)
	if err != nil {
		vlog.Fatalf("BindBenchmark(%q) failed: %v", address, err)
	}

	for i := 0; i < iterations; i++ {
		start := time.Now()
		result, err := stub.Echo(rt.R().TODOContext(), payload)
		elapsed := time.Since(start)
		if err != nil {
			vlog.Fatalf("Echo failed: %v", err)
		}
		if !bytes.Equal(payload, result) {
			vlog.Fatalf("Echo return different payload: got %v, expected %v", result, payload)
		}
		if log != nil {
			log.Write([]byte(fmt.Sprintf("CallEcho %d %d\n", i, elapsed)))
		}
	}
}

// CallEchoStream calls the EchoStream method 'rpcCount' times. Each iteration
// sends 'messageCount' messages on the stream and receives the same number
// back. Each message has the given payload size. Optionally logs the result.
func CallEchoStream(address string, rpcCount, messageCount, payloadSize int, log io.Writer) {
	payload := make([]byte, payloadSize)
	for _, i := range payload {
		payload[i] = byte(i & 0xff)
	}

	stub, err := BindBenchmark(address)
	if err != nil {
		vlog.Fatalf("BindBenchmark(%q) failed: %v", address, err)
	}

	for i := 0; i < rpcCount; i++ {
		start := time.Now()
		stream, err := stub.EchoStream(rt.R().TODOContext(), veyron2.CallTimeout(time.Hour))
		if err != nil {
			vlog.Fatalf("EchoStream failed: %v", err)
		}
		done := make(chan error, 1)
		go func() {
			for stream.Advance() {
				chunk := stream.Value()
				if err == io.EOF {
					done <- nil
					return
				}
				if err != nil {
					done <- err
					return
				}
				if !bytes.Equal(payload, chunk) {
					done <- fmt.Errorf("Recv got different payload: got %v, expected %v", chunk, payload)
					return
				}
			}

			done <- stream.Err()
		}()
		for j := 0; j < messageCount; j++ {
			if err = stream.Send(payload); err != nil {
				vlog.Fatalf("Send failed: %v", err)
			}
		}
		if err = stream.CloseSend(); err != nil {
			vlog.Fatalf("CloseSend() failed: %v", err)
		}
		if err = <-done; err != nil {
			vlog.Fatalf("%v", err)
		}

		if err = stream.Finish(); err != nil {
			vlog.Fatalf("Finish failed: %v", err)
		}
		elapsed := time.Since(start)
		if log != nil {
			log.Write([]byte(fmt.Sprintf("CallEchoStream %d %d\n", i, elapsed)))
		}
	}
}
