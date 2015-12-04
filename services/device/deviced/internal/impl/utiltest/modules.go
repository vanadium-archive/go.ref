// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package utiltest

import (
	"fmt"
	"os"
	goexec "os/exec"
	"strings"
	"testing"

	"v.io/v23"
	"v.io/v23/rpc"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/services/device/deviced/internal/starter"
	"v.io/x/ref/services/device/deviced/internal/versioning"
	"v.io/x/ref/services/device/internal/config"
	"v.io/x/ref/services/device/internal/suid"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
)

const (
	RedirectEnv    = "DEVICE_MANAGER_DONT_REDIRECT_STDOUT_STDERR"
	TestEnvVarName = "V23_RANDOM_ENV_VALUE"
	NoPairingToken = ""
)

// ExecScript launches the script passed as argument.
var ExecScript = modules.Register(func(env *modules.Env, args ...string) error {
	if want, got := 1, len(args); want != got {
		logger.Global().Fatalf("ExecScript expected %d arguments, got %d instead", want, got)
	}
	script := args[0]
	osenv := []string{RedirectEnv + "=1"}
	if env.Vars["PAUSE_BEFORE_STOP"] == "1" {
		osenv = append(osenv, "PAUSE_BEFORE_STOP=1")
	}

	cmd := goexec.Cmd{
		Path:   script,
		Env:    osenv,
		Stdin:  env.Stdin,
		Stderr: env.Stderr,
		Stdout: env.Stdout,
	}
	return cmd.Run()
}, "ExecScript")

// DeviceManager sets up a device manager server.  It accepts the name to
// publish the server under as an argument.  Additional arguments can optionally
// specify device manager config settings.
var DeviceManager = modules.Register(deviceManagerFunc, "DeviceManager")

func deviceManagerFunc(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	if len(args) == 0 {
		ctx.Fatalf("deviceManager expected at least an argument")
	}
	publishName := args[0]
	args = args[1:]
	defer fmt.Fprintf(env.Stdout, "%v terminated\n", publishName)
	defer ctx.VI(1).Infof("%v terminated", publishName)
	defer shutdown()

	// Satisfy the contract described in doc.go by passing the config state
	// through to the device manager dispatcher constructor.
	configState, err := config.Load()
	if err != nil {
		ctx.Fatalf("Failed to decode config state: %v", err)
	}

	// This exemplifies how to override or set specific config fields, if,
	// for example, the device manager is invoked 'by hand' instead of via a
	// script prepared by a previous version of the device manager.
	var pairingToken string
	if len(args) > 0 {
		if want, got := 4, len(args); want > got {
			ctx.Fatalf("expected atleast %d additional arguments, got %d instead: %q", want, got, args)
		}
		configState.Root, configState.Helper, configState.Origin, configState.CurrentLink = args[0], args[1], args[2], args[3]
		if len(args) > 4 {
			pairingToken = args[4]
		}
	}
	// We grab the shutdown channel at this point in order to ensure that we
	// register a listener for the app cycle manager Stop before we start
	// running the device manager service.  Otherwise, any device manager
	// method that calls Stop on the app cycle manager (e.g. the Stop RPC)
	// will precipitate an immediate process exit.
	shutdownChan := signals.ShutdownOnSignals(ctx)
	listenSpec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	claimableEps, stop, err := starter.Start(ctx, starter.Args{
		Namespace: starter.NamespaceArgs{
			ListenSpec: listenSpec,
		},
		Device: starter.DeviceArgs{
			Name:            publishName,
			ListenSpec:      listenSpec,
			ConfigState:     configState,
			TestMode:        strings.HasSuffix(fmt.Sprint(v23.GetPrincipal(ctx).BlessingStore().Default()), "/testdm"),
			RestartCallback: func() { fmt.Println("restart handler") },
			PairingToken:    pairingToken,
		},
		// TODO(rthellend): Wire up the local mounttable like the real device
		// manager, i.e. mount the device manager and the apps on it, and mount
		// the local mounttable in the global namespace.
		// MountGlobalNamespaceInLocalNamespace: true,
	})
	if err != nil {
		ctx.Errorf("starter.Start failed: %v", err)
		return err
	}
	defer stop()
	// Update the namespace roots to remove the server blessing from the
	// endpoints.  This is needed to be able to publish into the 'global'
	// mounttable before we have compatible credentials.
	ctx, err = SetNamespaceRootsForUnclaimedDevice(ctx)
	if err != nil {
		return err
	}
	// Manually mount the claimable service in the 'global' mounttable.
	for _, ep := range claimableEps {
		v23.GetNamespace(ctx).Mount(ctx, "claimable", ep.Name(), 0)
	}
	fmt.Fprintf(env.Stdout, "ready:%d\n", os.Getpid())

	<-shutdownChan
	if val, present := env.Vars["PAUSE_BEFORE_STOP"]; present && val == "1" {
		modules.WaitForEOF(env.Stdin)
	}
	// TODO(ashankar): Figure out a way to incorporate this check in the test.
	// if impl.DispatcherLeaking(dispatcher) {
	//	ctx.Fatalf("device manager leaking resources")
	// }
	return nil
}

// This is the same as DeviceManager above, except that it has a different major version number
var DeviceManagerV10 = modules.Register(func(env *modules.Env, args ...string) error {
	versioning.CurrentVersion = versioning.Version{10, 0} // Set the version number to 10.0
	return deviceManagerFunc(env, args...)
}, "DeviceManagerV10")

func TestMainImpl(m *testing.M) {
	isSuidHelper := len(os.Getenv("V23_SUIDHELPER_TEST")) > 0
	if modules.IsChildProcess() && !isSuidHelper {
		if err := modules.Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

// TestSuidHelper is testing boilerplate for suidhelper that does not
// create a runtime because the suidhelper is not a Vanadium application.
func TestSuidHelperImpl(t *testing.T) {
	if os.Getenv("V23_SUIDHELPER_TEST") != "1" {
		return
	}
	logger.Global().VI(1).Infof("TestSuidHelper starting")
	if err := suid.Run(os.Environ()); err != nil {
		logger.Global().Fatalf("Failed to Run() setuidhelper: %v", err)
	}
}
