package main

import (
	"flag"
	"time"

	"v.io/lib/cmdline"

	vexec "v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
	"v.io/core/veyron2"
	"v.io/core/veyron2/mgmt"
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

	var testMode bool
	// If this device manager was started by another device manager, it must
	// be part of a self update to test that this binary works. In that
	// case, we need to disable a lot of functionality.
	if handle, err := vexec.GetChildHandle(); err == nil {
		if _, err := handle.Config.Get(mgmt.ParentNameConfigKey); err == nil {
			testMode = true
			vlog.Infof("TEST MODE")
		}
	}

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
	dispatcher, err := impl.NewDispatcher(veyron2.GetPrincipal(ctx), configState, testMode, func() { exitErr = cmdline.ErrExitCode(*restartExitCode) })
	if err != nil {
		vlog.Errorf("Failed to create dispatcher: %v", err)
		return err
	}
	var publishName string
	if testMode == false {
		publishName = *publishAs
	}
	if err := server.ServeDispatcher(publishName, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", publishName, err)
		return err
	}
	vlog.VI(0).Infof("Device manager published as: %v", publishName)
	impl.InvokeCallback(ctx, name)

	// Wait until shutdown.  Ignore duplicate signals (sent by agent and
	// received as part of process group).
	signals.SameSignalTimeWindow = 500 * time.Millisecond
	<-signals.ShutdownOnSignals(ctx)

	return exitErr
}
