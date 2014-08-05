package main

import (
	"veyron/tools/qsh/impl"

	"flag"
	"log"
	"os"

	"veyron2/rt"
)

var flagStoreName = flag.String("targetstore", "", "Store object name")

const usage = `
Synopsis: qsh [--targetstore=<store in veyron namespace>] query...

Runs each given query against the specified Veyron store instance. If
no target store is specified on the command line, qsh expects the
environment variable VEYRON_STORE to specify the store to query.
`

func main() {
	rt.Init()

	// TODO(rjkroege@google.com): Handle ^c nicely.
	flag.Parse()
	queryStringArgs := flag.Args()

	// Command line overrides.
	storeName := *flagStoreName
	if storeName == "" {
		storeName = os.ExpandEnv("${VEYRON_STORE}")
	}

	if storeName == "" {
		log.Fatalf("qsh: No store specified\n" + usage)
	}

	err := impl.Runquery(storeName, queryStringArgs[0])
	if err != nil {
		log.Printf("qsh: When attempting query: \"%s\" experienced an error: ", queryStringArgs[0], err.Error())
	}
}
