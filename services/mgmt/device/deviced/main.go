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
	publishAs   = flag.String("name", "", "name to publish the device manager at")
	installSelf = flag.Bool("install_self", false, "perform installation using environment and command-line flags")
	installFrom = flag.String("install_from", "", "if not-empty, perform installation from the provided application envelope object name")
	uninstall   = flag.Bool("uninstall", false, "uninstall the installation specified via the config")
)

func main() {
	flag.Parse()
	runtime, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()

	if len(*installFrom) > 0 {
		if err := impl.InstallFrom(*installFrom); err != nil {
			vlog.Errorf("InstallFrom failed: %v", err)
			os.Exit(1)
		}
		return
	}

	if *installSelf {
		// TODO(caprita): Make the flags survive updates.
		if err := impl.SelfInstall(flag.Args(), os.Environ()); err != nil {
			vlog.Errorf("SelfInstall failed: %v", err)
			os.Exit(1)
		}
		return
	}

	if *uninstall {
		if err := impl.Uninstall(); err != nil {
			vlog.Errorf("Uninstall failed: %v", err)
			os.Exit(1)
		}
		return
	}

	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	defer server.Stop()
	endpoints, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", roaming.ListenSpec, err)
	}
	name := naming.JoinAddressName(endpoints[0].String(), "")
	vlog.VI(0).Infof("Device manager object name: %v", name)
	configState, err := config.Load()
	if err != nil {
		vlog.Fatalf("Failed to load config passed from parent: %v", err)
		return
	}
	configState.Name = name
	// TODO(caprita): We need a way to set config fields outside of the
	// update mechanism (since that should ideally be an opaque
	// implementation detail).
	dispatcher, err := impl.NewDispatcher(runtime.Principal(), configState)
	if err != nil {
		vlog.Fatalf("Failed to create dispatcher: %v", err)
	}
	if err := server.ServeDispatcher(*publishAs, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", *publishAs, err)
	}
	vlog.VI(0).Infof("Device manager published as: %v", *publishAs)
	impl.InvokeCallback(runtime.NewContext(), name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals(runtime)
}
