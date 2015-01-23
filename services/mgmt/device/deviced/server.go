package main

import (
	"flag"
	"time"

	"v.io/lib/cmdline"

	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
	"v.io/core/veyron2"
	"v.io/core/veyron2/vlog"
)

var (
	// TODO(caprita): publishAs and restartExitCode should be provided by the
	// config?
	publishAs       = flag.String("name", "", "name to publish the device manager at")
	restartExitCode = flag.Int("restart_exit_code", 0, "exit code to return when device manager should be restarted")
)

func runServer(*cmdline.Command, []string) error {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return err
	}
	defer server.Stop()
	ls := veyron2.GetListenSpec(ctx)
	endpoints, err := server.Listen(ls)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", ls, err)
		return err
	}
	name := endpoints[0].Name()
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
	dispatcher, err := impl.NewDispatcher(veyron2.GetPrincipal(ctx), configState, func() { exitErr = cmdline.ErrExitCode(*restartExitCode) })
	if err != nil {
		vlog.Errorf("Failed to create dispatcher: %v", err)
		return err
	}
	if err := server.ServeDispatcher(*publishAs, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *publishAs, err)
		return err
	}
	vlog.VI(0).Infof("Device manager published as: %v", *publishAs)
	impl.InvokeCallback(ctx, name)

	// Wait until shutdown.  Ignore duplicate signals (sent by agent and
	// received as part of process group).
	signals.SameSignalTimeWindow = 500 * time.Millisecond
	<-signals.ShutdownOnSignals(ctx)

	return exitErr
}
