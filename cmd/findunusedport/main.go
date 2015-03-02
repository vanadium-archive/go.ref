package main

// findunusedport finds a random unused TCP port in the range 1k to 64k and prints it to standard out.

import (
	"fmt"

	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/testutil"
)

func main() {
	port, err := testutil.FindUnusedPort()
	if err != nil {
		vlog.Fatalf("can't find unused port: %v\n", err)
	} else if port == 0 {
		vlog.Fatalf("can't find unused port")
	} else {
		fmt.Println(port)
	}
}
