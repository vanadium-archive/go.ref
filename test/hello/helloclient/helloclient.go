// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command helloclient is the simplest possible client.  It is mainly used in simple
// regression tests.
package main

import (
	"flag"
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/verror"
	_ "v.io/x/ref/profiles"
)

var name *string = flag.String("name", "", "Name of the hello server")

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var result string
	err := v23.GetClient(ctx).Call(ctx, *name, "Hello", nil, []interface{}{&result})
	if err != nil {
		panic(verror.DebugString(err))
	}

	if result != "hello" {
		panic(fmt.Sprintf("Unexpected result.  Wanted %q, got %q", "hello", result))
	}
}
