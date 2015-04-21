// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"bytes"
	"fmt"
	"math/rand"
	"time"

	"v.io/v23/context"

	"v.io/x/lib/vlog"
	"v.io/x/ref/profiles/internal/rpc/stress"
)

// CallEcho calls 'Echo' method with the given payload size for the given time
// duration and returns the number of iterations.
func CallEcho(ctx *context.T, server string, payloadSize int, duration time.Duration) uint64 {
	stub := stress.StressClient(server)
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i & 0xff)
	}

	var iterations uint64
	start := time.Now()
	for {
		got, err := stub.Echo(ctx, payload)
		if err != nil {
			vlog.Fatalf("Echo failed: %v", err)
		}
		if !bytes.Equal(got, payload) {
			vlog.Fatalf("Echo returned %v, but expected %v", got, payload)
		}
		iterations++

		if time.Since(start) >= duration {
			break
		}
	}
	return iterations
}

// CallSum calls 'Sum' method with a randomly generated payload.
func CallSum(ctx *context.T, server string, maxPayloadSize int, stats *stress.SumStats) {
	stub := stress.StressClient(server)
	arg, err := newSumArg(maxPayloadSize)
	if err != nil {
		vlog.Fatalf("new arg failed: %v", err)
	}

	got, err := stub.Sum(ctx, arg)
	if err != nil {
		vlog.Fatalf("Sum failed: %v", err)
	}

	wanted, _ := doSum(&arg)
	if !bytes.Equal(got, wanted) {
		vlog.Fatalf("Sum returned %v, but expected %v", got, wanted)
	}
	stats.SumCount++
	stats.BytesSent += uint64(lenSumArg(&arg))
	stats.BytesRecv += uint64(len(got))
}

// CallSumStream calls 'SumStream' method. Each iteration sends up to
// 'maxChunkCnt' chunks on the stream and receives the same number of
// sums back.
func CallSumStream(ctx *context.T, server string, maxChunkCnt, maxPayloadSize int, stats *stress.SumStats) {
	stub := stress.StressClient(server)
	stream, err := stub.SumStream(ctx)
	if err != nil {
		vlog.Fatalf("Stream failed: %v", err)
	}

	chunkCnt := rand.Intn(maxChunkCnt) + 1
	args := make([]stress.SumArg, chunkCnt)
	done := make(chan error, 1)
	go func() {
		defer close(done)

		recvS := stream.RecvStream()
		i := 0
		for ; recvS.Advance(); i++ {
			got := recvS.Value()
			wanted, _ := doSum(&args[i])
			if !bytes.Equal(got, wanted) {
				done <- fmt.Errorf("RecvStream returned %v, but expected %v", got, wanted)
				return
			}
			stats.BytesRecv += uint64(len(got))
		}
		switch err := recvS.Err(); {
		case err != nil:
			done <- err
		case i != chunkCnt:
			done <- fmt.Errorf("RecvStream returned %d chunks, but expected %d", i, chunkCnt)
		default:
			done <- nil
		}
	}()

	sendS := stream.SendStream()
	for i := 0; i < chunkCnt; i++ {
		arg, err := newSumArg(maxPayloadSize)
		if err != nil {
			vlog.Fatalf("new arg failed: %v", err)
		}
		args[i] = arg

		if err = sendS.Send(arg); err != nil {
			vlog.Fatalf("SendStream failed to send: %v", err)
		}
		stats.BytesSent += uint64(lenSumArg(&arg))
	}
	if err = sendS.Close(); err != nil {
		vlog.Fatalf("SendStream failed to close: %v", err)
	}
	if err = <-done; err != nil {
		vlog.Fatalf("%v", err)
	}
	if err = stream.Finish(); err != nil {
		vlog.Fatalf("Stream failed to finish: %v", err)
	}
	stats.SumStreamCount++
}
