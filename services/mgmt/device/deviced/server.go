package main

import (
	"flag"
	"net"
	"strconv"
	"time"

	"v.io/lib/cmdline"

	vexec "v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/signals"
	_ "v.io/core/veyron/profiles/roaming"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
	mounttable "v.io/core/veyron/services/mounttable/lib"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/vlog"
)

var (
	// TODO(caprita): publishAs and restartExitCode should be provided by the
	// config?
	publishAs       = flag.String("name", "", "name to publish the device manager at")
	restartExitCode = flag.Int("restart_exit_code", 0, "exit code to return when device manager should be restarted")
	nhName          = flag.String("neighborhood_name", "", `if provided, it will enable sharing with the local neighborhood with the provided name. The address of the local mounttable will be published to the neighboorhood and everything in the neighborhood will be visible on the local mounttable.`)
	dmPort          = flag.Int("deviced_port", 0, "the port number of assign to the device manager service. The hostname/IP address part of --veyron.tcp.address is used along with this port. By default, the port is assigned by the OS.")
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
	ls := veyron2.GetListenSpec(ctx)
	if testMode {
		ls.Addrs = ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}
		ls.Proxy = ""
	}

	var publishName string
	if !testMode {
		publishName = *publishAs
	}
	// TODO(rthellend): Figure out how to integrate the mounttable ACLs.
	mtName, stop, err := mounttable.StartServers(ctx, ls, publishName, *nhName, "" /* ACL File */)
	if err != nil {
		vlog.Errorf("mounttable.StartServers failed: %v", err)
		return err
	}
	defer stop()
	vlog.VI(0).Infof("Local mounttable published as: %v", publishName)

	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return err
	}
	defer server.Stop()

	// Bring up the device manager with the same address as the mounttable.
	dmListenSpec := ls
	for i, a := range ls.Addrs {
		host, _, err := net.SplitHostPort(a.Address)
		if err != nil {
			vlog.Errorf("net.SplitHostPort(%v) failed: %v", err)
			return err
		}
		dmListenSpec.Addrs[i].Address = net.JoinHostPort(host, strconv.Itoa(*dmPort))
	}
	endpoints, err := server.Listen(dmListenSpec)
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
	dispatcher, err := impl.NewDispatcher(veyron2.GetPrincipal(ctx), configState, mtName, testMode, func() { exitErr = cmdline.ErrExitCode(*restartExitCode) })
	if err != nil {
		vlog.Errorf("Failed to create dispatcher: %v", err)
		return err
	}
	dmPublishName := naming.Join(mtName, "devmgr")
	if err := server.ServeDispatcher(dmPublishName, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", dmPublishName, err)
		return err
	}
	vlog.VI(0).Infof("Device manager published as: %v", dmPublishName)

	// Mount the global namespace in the local namespace as "global".
	ns := veyron2.GetNamespace(ctx)
	for _, root := range ns.Roots() {
		go func(r string) {
			for {
				err := ns.Mount(ctx, naming.Join(mtName, "global"), r, 0 /* forever */, naming.ServesMountTableOpt(true))
				if err == nil {
					break
				}
				vlog.Infof("Failed to Mount global namespace: %v", err)
				time.Sleep(time.Second)
			}
		}(root)
	}

	impl.InvokeCallback(ctx, name)

	// Wait until shutdown.  Ignore duplicate signals (sent by agent and
	// received as part of process group).
	signals.SameSignalTimeWindow = 500 * time.Millisecond
	<-signals.ShutdownOnSignals(ctx)

	return exitErr
}
