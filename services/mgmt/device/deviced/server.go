package main

import (
	"flag"

	"v.io/lib/cmdline"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"
)

var (
	// TODO(caprita): publishAs and stopExitCode should be provided by the
	// config?
	publishAs    = flag.String("name", "", "name to publish the device manager at")
	stopExitCode = flag.Int("stop_exit_code", 0, "exit code to return when stopped via the Stop RPC")
)

func runServer(*cmdline.Command, []string) error {
	runtime, err := rt.New()
	if err != nil {
		vlog.Errorf("Could not initialize runtime: %v", err)
		return err
	}
	defer runtime.Cleanup()

	ctx := runtime.NewContext()

	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return err
	}
	defer server.Stop()
	endpoints, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		return err
	}
	name := naming.JoinAddressName(endpoints[0].String(), "")
	vlog.VI(0).Infof("Device manager object name: %v", name)
	configState, err := config.Load()
	if err != nil {
		vlog.Errorf("Failed to load config passed from parent: %v", err)
		return err
	}
	configState.Name = name
	// TODO(caprita): We need a way to set config fields outside of the
	// update mechanism (since that should ideally be an opaque
	// implementation detail).

	var exitErr error
	dispatcher, err := impl.NewDispatcher(runtime.Principal(), configState, func() { exitErr = cmdline.ErrExitCode(*stopExitCode) })
	if err != nil {
		vlog.Errorf("Failed to create dispatcher: %v", err)
		return err
	}
	if err := server.ServeDispatcher(*publishAs, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *publishAs, err)
		return err
	}
	vlog.VI(0).Infof("Device manager published as: %v", *publishAs)
	impl.InvokeCallback(runtime.NewContext(), name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)

	return exitErr
}
