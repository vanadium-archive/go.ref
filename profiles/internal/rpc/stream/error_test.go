// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream_test

import (
	"net"
	"testing"

	"v.io/v23/verror"

	"v.io/x/ref/profiles/internal/rpc/stream"
)

func TestTimeoutError(t *testing.T) {
	e := verror.Register(".test", verror.NoRetry, "hello{:3}")
	timeoutErr := stream.NewNetError(verror.New(e, nil, "world"), true, false)

	// TimeoutError implements error & net.Error. We test that it
	// implements error by assigning timeoutErr to err which is of type error.
	var err error
	err = timeoutErr

	neterr, ok := err.(net.Error)
	if !ok {
		t.Fatalf("%T not a net.Error", err)
	}

	if got, want := neterr.Timeout(), true; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := neterr.Error(), "hello: world"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
