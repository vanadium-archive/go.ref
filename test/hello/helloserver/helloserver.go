// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
)

var name string

func main() {
	cmdHelloServer.Flags.StringVar(&name, "name", "", "Name to publish under.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdHelloServer)
}

var cmdHelloServer = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runHelloServer),
	Name:   "helloserver",
	Short:  "Simple server mainly used in regression tests.",
	Long: `
Command helloserver is a simple server mainly used in regression tests.
`,
}

type helloServer struct{}

func (*helloServer) Hello(ctx *context.T, call rpc.ServerCall) (string, error) {
	return "hello", nil
}

func runHelloServer(ctx *context.T, env *cmdline.Env, args []string) error {
	server, err := v23.NewServer(ctx)
	if err != nil {
		return fmt.Errorf("NewServer: %v", err)
	}
	eps, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		return fmt.Errorf("Listen: %v", err)
	}
	if len(eps) > 0 {
		fmt.Printf("SERVER_NAME=%s\n", eps[0].Name())
	} else {
		fmt.Println("SERVER_NAME=proxy")
	}
	if err := server.Serve(name, &helloServer{}, security.AllowEveryone()); err != nil {
		return fmt.Errorf("Serve: %v", err)
	}
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
