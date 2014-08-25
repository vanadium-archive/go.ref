package main

import (
	"flag"
	"log"

	"veyron/tools/qsh/impl"

	"veyron2/rt"
)

var flagQueryRoot = flag.String("queryroot", "",
	"An object name in the store to serve as the root of the query.")

const usage = `
Synopsis: qsh --queryroot=<object in the store> query...

Runs a given query starting at the given root.
`

func main() {
	r := rt.Init()

	// TODO(rjkroege@google.com): Handle ^c nicely.
	flag.Parse()
	queryStringArgs := flag.Args()
	if len(queryStringArgs) != 1 {
		log.Fatalf("qsh: Expected only one query arg\n" + usage)
	}

	queryRoot := *flagQueryRoot
	if queryRoot == "" {
		log.Fatalf("qsh: No queryroot specified\n" + usage)
	}

	err := impl.RunQuery(r.NewContext(), queryRoot, queryStringArgs[0])
	if err != nil {
		log.Printf("qsh: When attempting query: \"%s\" experienced an error: ", queryStringArgs[0], err.Error())
	}
}
