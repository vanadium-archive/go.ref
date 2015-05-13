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
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
)

var name, store string

func main() {
	cmdProfileD.Flags.StringVar(&name, "name", "", "Name to mount the profile repository as.")
	cmdProfileD.Flags.StringVar(&store, "store", "", "Local directory to store profiles in.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdProfileD)
}

var cmdProfileD = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runProfileD),
	Name:   "profiled",
	Short:  "Runs the profile daemon.",
	Long: `
Command profiled runs the profile daemon, which implements the
v.io/x/ref/services/repository.Profile interface.
`,
}

func runProfileD(ctx *context.T, env *cmdline.Env, args []string) error {
	if store == "" {
		return env.UsageErrorf("Specify a directory for storing profiles using --store=<name>")
	}

	server, err := v23.NewServer(ctx)
	if err != nil {
		return fmt.Errorf("NewServer() failed: %v", err)
	}

	dispatcher, err := NewDispatcher(store, securityflag.NewAuthorizerOrDie())
	if err != nil {
		return fmt.Errorf("NewDispatcher() failed: %v", err)
	}

	ls := v23.GetListenSpec(ctx)
	endpoint, err := server.Listen(ls)
	if err != nil {
		return fmt.Errorf("Listen(%s) failed: %v", ls, err)
	}
	if err := server.ServeDispatcher(name, dispatcher); err != nil {
		return fmt.Errorf("ServeDispatcher(%v) failed: %v", name, err)
	}
	vlog.Infof("Profile repository running at endpoint=%q", endpoint)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
	return nil
}
