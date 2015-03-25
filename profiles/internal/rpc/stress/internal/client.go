// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"bytes"
	crand "crypto/rand"
	"fmt"
	"math/rand"

	"v.io/v23/context"
	"v.io/x/lib/vlog"

	"v.io/x/ref/profiles/internal/rpc/stress"
)

func newArg(maxPayloadSize int) (stress.Arg, error) {
	var arg stress.Arg
	arg.ABool = rand.Intn(2) == 0
	arg.AInt64 = rand.Int63()
	arg.AListOfBytes = make([]byte, rand.Intn(maxPayloadSize)+1)
	_, err := crand.Read(arg.AListOfBytes)
	return arg, err
}

// CallSum calls 'Sum' method with a randomly generated payload.
func CallSum(ctx *context.T, server string, maxPayloadSize int) {
	stub := stress.StressClient(server)
	arg, err := newArg(maxPayloadSize)
	if err != nil {
		vlog.Fatalf("new arg failed: %v", err)
	}

	got, err := stub.Sum(ctx, arg)
	if err != nil {
		vlog.Fatalf("Sum failed: %v", err)
	}

	wanted, _ := doSum(arg)
	if !bytes.Equal(got, wanted) {
		vlog.Fatalf("Sum returned %v, but expected %v", got, wanted)
	}
}

// CallSumStream calls 'SumStream' method. Each iteration sends up to
// 'maxChunkCnt' chunks on the stream and receives the same number of
// sums back.
func CallSumStream(ctx *context.T, server string, maxChunkCnt, maxPayloadSize int) {
	stub := stress.StressClient(server)
	stream, err := stub.SumStream(ctx)
	if err != nil {
		vlog.Fatalf("Stream failed: %v", err)
	}

	chunkCnt := rand.Intn(maxChunkCnt) + 1
	args := make([]stress.Arg, chunkCnt)
	done := make(chan error, 1)
	go func() {
		defer close(done)

		recvS := stream.RecvStream()
		i := 0
		for ; recvS.Advance(); i++ {
			got := recvS.Value()
			wanted, _ := doSum(args[i])
			if !bytes.Equal(got, wanted) {
				done <- fmt.Errorf("RecvStream returned %v, but expected %v", got, wanted)
				return
			}
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
		arg, err := newArg(maxPayloadSize)
		if err != nil {
			vlog.Fatalf("new arg failed: %v", err)
		}
		args[i] = arg

		if err = sendS.Send(arg); err != nil {
			vlog.Fatalf("SendStream failed to send: %v", err)
		}
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
}
