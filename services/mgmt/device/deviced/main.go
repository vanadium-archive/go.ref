package main

import (
	"flag"
	"os"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
)

var (
	// TODO(caprita): publishAs and stopExitCode should be provided by the config?
	publishAs    = flag.String("name", "", "name to publish the device manager at")
	stopExitCode = flag.Int("stop_exit_code", 0, "exit code to return when stopped via the Stop RPC")
	installSelf  = flag.Bool("install_self", false, "perform installation using environment and command-line flags")
	installFrom  = flag.String("install_from", "", "if not-empty, perform installation from the provided application envelope object name")
	uninstall    = flag.Bool("uninstall", false, "uninstall the installation specified via the config")
)

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()
	flag.Parse()
	runtime, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()

	if len(*installFrom) > 0 {
		if err := impl.InstallFrom(*installFrom); err != nil {
			vlog.Errorf("InstallFrom failed: %v", err)
			exitCode = 1
		}
		return
	}

	if *installSelf {
		// TODO(caprita): Make the flags survive updates.
		if err := impl.SelfInstall(flag.Args(), os.Environ()); err != nil {
			vlog.Errorf("SelfInstall failed: %v", err)
			exitCode = 1
		}
		return
	}

	if *uninstall {
		if err := impl.Uninstall(); err != nil {
			vlog.Errorf("Uninstall failed: %v", err)
			exitCode = 1
		}
		return
	}

	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		exitCode = 1
		return
	}
	defer server.Stop()
	endpoints, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		exitCode = 1
		return
	}
	name := naming.JoinAddressName(endpoints[0].String(), "")
	vlog.VI(0).Infof("Device manager object name: %v", name)
	configState, err := config.Load()
	if err != nil {
		vlog.Errorf("Failed to load config passed from parent: %v", err)
		exitCode = 1
		return
	}
	configState.Name = name
	// TODO(caprita): We need a way to set config fields outside of the
	// update mechanism (since that should ideally be an opaque
	// implementation detail).
	dispatcher, err := impl.NewDispatcher(runtime.Principal(), configState, func() { exitCode = *stopExitCode })
	if err != nil {
		vlog.Errorf("Failed to create dispatcher: %v", err)
		exitCode = 1
		return
	}
	if err := server.ServeDispatcher(*publishAs, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *publishAs, err)
		exitCode = 1
		return
	}
	vlog.VI(0).Infof("Device manager published as: %v", *publishAs)
	impl.InvokeCallback(runtime.NewContext(), name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(runtime)
}
