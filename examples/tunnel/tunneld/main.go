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
	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
	"v.io/x/ref/examples/tunnel"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"

	_ "v.io/x/ref/runtime/factories/roaming"
)

var name string

func main() {
	cmdRoot.Flags.StringVar(&name, "name", "", "Name to publish the server as.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

var cmdRoot = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runTunnelD),
	Name:   "tunneld",
	Short:  "Runs the tunneld daemon",
	Long: `
Command tunneld runs the tunneld daemon, which implements the Tunnel interface.
`,
}

func runTunnelD(ctx *context.T, env *cmdline.Env, args []string) error {
	auth := securityflag.NewAuthorizerOrDie()
	server, err := v23.NewServer(ctx)
	if err != nil {
		return fmt.Errorf("NewServer failed: %v", err)
	}
	defer server.Stop()

	listenSpec := v23.GetListenSpec(ctx)
	if _, err := server.Listen(listenSpec); err != nil {
		return fmt.Errorf("Listen(%v) failed: %v", listenSpec, err)
	}
	if err := server.Serve(name, tunnel.TunnelServer(&T{}), auth); err != nil {
		return fmt.Errorf("Serve(%v) failed: %v", name, err)
	}
	status := server.Status()
	vlog.Infof("Listening on: %v", status.Endpoints)
	if len(status.Endpoints) > 0 {
		fmt.Printf("NAME=%s\n", status.Endpoints[0].Name())
	}
	vlog.Infof("Published as %q", name)

	<-signals.ShutdownOnSignals(ctx)
	return nil
}
