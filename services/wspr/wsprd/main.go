// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build wspr
//
// We restrict wsprd to a special build-tag in order to enable
// security.OverrideCaveatValidation, which isn't generally available.
//
// Manually run the following to generate the doc.go file.  This isn't a
// go:generate comment, since generate also needs to be run with -tags=wspr,
// which is troublesome for presubmit tests.
//
// cd $V23_ROOT/release/go/src && go run v.io/x/lib/cmdline/testdata/gendoc.go -tags=wspr v.io/x/ref/services/wspr/wsprd -help

package main

import (
	"fmt"
	"net"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	// TODO(cnicolaou,benj): figure out how to support roaming as a chrome plugin
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/wspr/wsprlib"
)

var (
	port   int
	identd string
)

func init() {
	wsprlib.OverrideCaveatValidation()
	cmdWsprD.Flags.IntVar(&port, "port", 8124, "Port to listen on.")
	cmdWsprD.Flags.StringVar(&identd, "identd", "", "Name of identd server.")
}

func main() {
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdWsprD)
}

var cmdWsprD = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runWsprD),
	Name:   "wsprd",
	Short:  "Runs the wspr web socket proxy daemon",
	Long: `
Command wsprd runs the wspr web socket proxy daemon.
`,
}

func runWsprD(ctx *context.T, env *cmdline.Env, args []string) error {
	listenSpec := v23.GetListenSpec(ctx)
	proxy := wsprlib.NewWSPR(ctx, port, &listenSpec, identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	nhost, nport, _ := net.SplitHostPort(addr.String())
	fmt.Printf("Listening on host: %s port: %s\n", nhost, nport)
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
