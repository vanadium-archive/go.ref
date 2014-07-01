// This example client illustrates how to use both the IDL stubbed API for
// talking to a simple service (inspector) as well as the stubbles API.
// It decides which to use based on the name of the service. If the name
// contains a component (anywhere with it, other than the address)
// "stubbed" then it will use the IDL stubs or if it contains a component
// "stubless" then it will use the naked API. If neither is found then
// an error encountered.
// The inspector service supports three subtrees: files, proc and dev
// representing the file system, /proc and /dev on the target system. The
// filesystem is relative to the current working directory of the server.
// The client will list either just the names, or optionally (-l) various
// metadata for each item returned.
// The results are streamed back to the client.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"

	"veyron/lib/signals"

	"veyron/examples/inspector"
)

var (
	service string
	glob    string
	details bool
)

func init() {
	flag.StringVar(&service, "service", "", "service to use")
	flag.StringVar(&glob, "glob", "", "glob")
	flag.BoolVar(&details, "l", false, "details")
}

func main() {
	rt.Init()
	_, name := naming.SplitAddressName(service)
	// The presence of a name component of 'stubbed' or 'stubless'
	// determines whether the client should use IDL stubs or not.
	for _, component := range strings.Split(name, "/") {
		switch component {
		case "stubbed":
			stubbed()
			return
		case "stubless":
			stubless()
			return
		}
	}
	fmt.Fprintf(os.Stderr, "need to use one of '.../stubbed/...' or '.../stubless/...'\n")
	os.Exit(1)
}

// ls and lsdashl are idiomatic for use without stubs
func ls(call ipc.Call) {
	for {
		var n string
		if err := call.Recv(&n); err != nil {
			if err == io.EOF {
				break
			}
			vlog.Fatalf("unexpected streaming error: %q", err)
		} else {
			fmt.Printf("%s\n", n)
		}
	}
}

func lsdashl(call ipc.Call) {
	type details struct {
		Name    string
		Size    int64
		Mode    os.FileMode
		ModTime time.Time
		IsDir   bool
	}
	for {
		var n details
		if err := call.Recv(&n); err != nil {
			if err == io.EOF {
				break
			}
			vlog.Fatalf("unexpected streaming error: %q", err)
		} else {
			fmt.Printf("%s: %d %s %s%s\n", n.Name, n.Size, n.Mode, n.ModTime, map[bool]string{false: "", true: "/"}[n.IsDir])
		}
	}
}

func stubless() {
	client, err := rt.R().NewClient()
	if err != nil {
		vlog.Fatalf("failed to create new client: %q", err)
	}
	defer client.Close()
	call, err := client.StartCall(rt.R().NewContext(), service, "List", []interface{}{glob, details})
	if err != nil {
		vlog.Fatalf("failed to start call: %q", err)
	}
	go func() {
		<-signals.ShutdownOnSignals()
		call.Cancel()
	}()
	if details {
		lsdashl(call)
	} else {
		ls(call)
	}
	var verr error
	if err := call.Finish(&verr); err != nil && err != io.EOF {
		vlog.Fatalf("%q", err)
	}
	if verr != nil {
		vlog.Fatalf("%q", verr)
	}
}

// streamNames and streamDetails are idiomatic for use with stubs
func streamNames(stream inspector.InspectorLsStream) {
	for {
		if name, err := stream.Recv(); err != nil {
			if err == io.EOF {
				break
			}
			vlog.Fatalf("unexpected streaming error: %q", err)
		} else {
			fmt.Printf("%s\n", name)
		}
	}
	if err := stream.Finish(); err != nil && err != io.EOF {
		vlog.Fatalf("%q", err)
	}
}

func streamDetails(stream inspector.InspectorLsDetailsStream) {
	for {
		if details, err := stream.Recv(); err != nil {
			if err == io.EOF {
				break
			}
			vlog.Fatalf("unexpected streaming error: %q", err)
		} else {
			mode := os.FileMode(details.Mode)
			modtime := time.Unix(details.ModUnixSecs, int64(details.ModNano))
			fmt.Printf("%s: %d %s %s%s\n", details.Name, details.Size, mode, modtime, map[bool]string{false: "", true: "/"}[details.IsDir])
		}
	}
	if err := stream.Finish(); err != nil && err != io.EOF {
		vlog.Fatalf("%q", err)
	}

}

func stubbed() {
	inspector, err := inspector.BindInspector(service)
	if err != nil {
		vlog.Fatalf("failed to create new client: %q", err)
	}
	bail := func(err error) {
		vlog.Fatalf("failed to start call: %q", err)
	}
	ctx := rt.R().NewContext()
	if details {
		if stream, err := inspector.LsDetails(ctx, glob); err != nil {
			bail(err)
		} else {
			streamDetails(stream)
		}
	} else {
		if stream, err := inspector.Ls(ctx, glob); err != nil {
			bail(err)
		} else {
			streamNames(stream)
		}
	}
	// TODO(cnicolaou): stubs need to expose cancel method.
}
