package main

// suidhelper should be installed setuid root. Having done this, it will
// run the provided command as the specified user identity.
// suidhelper deliberately attempts to be as simple as possible to
// simplify reviewing it for security concerns.

import (
	"flag"
	"fmt"
	"os"

	"v.io/veyron/veyron/services/mgmt/suidhelper/impl"
)

func main() {
	flag.Parse()
	if err := impl.Run(os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed with:", err)
		// TODO(rjkroege): We should really only print the usage message
		// if the error is related to interpreting flags.
		flag.Usage()
	}
}
