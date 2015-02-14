// Package starter provides a single function that starts up servers for a
// mounttable and a device manager that is mounted on it.
package starter

import (
	"time"

	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/device/impl"
	mounttable "v.io/core/veyron/services/mounttable/lib"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/vlog"
)

type NamespaceArgs struct {
	Name       string         // Name to publish the mounttable service under.
	ListenSpec ipc.ListenSpec // ListenSpec for the server.
	ACLFile    string         // Path to the ACL file used by the mounttable.
	// Name in the local neighborhood on which to make the mounttable
	// visible. If empty, the mounttable will not be visible in the local
	// neighborhood.
	Neighborhood string
}

type DeviceArgs struct {
	Name            string         // Name to publish the device service under.
	ListenSpec      ipc.ListenSpec // ListenSpec for the device server.
	ConfigState     *config.State  // Configuration for the device.
	TestMode        bool           // Whether the device is running in test mode or not.
	RestartCallback func()         // Callback invoked when the device service is restarted.
	PairingToken    string         // PairingToken that a claimer needs to provide.
}

func (d *DeviceArgs) name(mt string) string {
	if d.Name != "" {
		return d.Name
	}
	return naming.Join(mt, "devmgr")
}

type Args struct {
	Namespace NamespaceArgs
	Device    DeviceArgs

	// If true, the global namespace will be made available on the
	// mounttable server under "global/".
	MountGlobalNamespaceInLocalNamespace bool
}

// Start creates servers for the mounttable and device services and links them together.
//
// Returns the callback to be invoked to shutdown the services on success, or
// an error on failure.
func Start(ctx *context.T, args Args) (func(), error) {
	// If the device has not yet been claimed, start the mounttable and
	// claimable service and wait for it to be claimed.
	// Once a device is claimed, close any previously running servers and
	// start a new mounttable and device service.
	if claimable, claimed := impl.NewClaimableDispatcher(ctx, args.Device.ConfigState, args.Device.PairingToken); claimable != nil {
		stopClaimable, err := startClaimableDevice(ctx, claimable, args)
		if err != nil {
			return nil, err
		}
		stop := make(chan struct{})
		stopped := make(chan struct{})
		go waitToBeClaimedAndStartClaimedDevice(ctx, stopClaimable, claimed, stop, stopped, args)
		return func() {
			close(stop)
			<-stopped
		}, nil
	}
	return startClaimedDevice(ctx, args)
}

func startClaimableDevice(ctx *context.T, dispatcher ipc.Dispatcher, args Args) (func(), error) {
	mtName, stopMT, err := startMounttable(ctx, args.Namespace)
	if err != nil {
		return nil, err
	}
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		stopMT()
		return nil, err
	}
	shutdown := func() {
		server.Stop()
		stopMT()
	}
	endpoints, err := server.Listen(args.Device.ListenSpec)
	if err != nil {
		shutdown()
		return nil, err
	}
	if err := server.ServeDispatcher(args.Device.name(mtName), dispatcher); err != nil {
		shutdown()
		return nil, err
	}
	vlog.Infof("Unclaimed device manager (%v) published as %v", endpoints[0].Name(), args.Device.name(mtName))
	return shutdown, nil
}

func waitToBeClaimedAndStartClaimedDevice(ctx *context.T, stopClaimable func(), claimed, stop <-chan struct{}, stopped chan<- struct{}, args Args) {
	// Wait for either the claimable service to complete, or be stopped
	defer close(stopped)
	select {
	case <-claimed:
		stopClaimable()
	case <-stop:
		stopClaimable()
		return
	}
	shutdown, err := startClaimedDevice(ctx, args)
	if err != nil {
		vlog.Errorf("Failed to start device service after it was claimed: %v", err)
		veyron2.GetAppCycle(ctx).Stop()
		return
	}
	defer shutdown()
	<-stop // Wait to be stopped
}

func startClaimedDevice(ctx *context.T, args Args) (func(), error) {
	mtName, stopMT, err := startMounttable(ctx, args.Namespace)
	stopDevice, err := startDeviceServer(ctx, args.Device, mtName)
	if err != nil {
		stopMT()
		vlog.Errorf("Failed to start device service: %v", err)
		return nil, err
	}
	if args.MountGlobalNamespaceInLocalNamespace {
		mountGlobalNamespaceInLocalNamespace(ctx, mtName)
	}
	impl.InvokeCallback(ctx, args.Device.ConfigState.Name)

	return func() {
		stopDevice()
		stopMT()
	}, nil
}

func startMounttable(ctx *context.T, n NamespaceArgs) (string, func(), error) {
	mtName, stopMT, err := mounttable.StartServers(ctx, n.ListenSpec, n.Name, n.Neighborhood, n.ACLFile)
	if err != nil {
		vlog.Errorf("mounttable.StartServers(%#v) failed: %v", n, err)
	} else {
		vlog.Infof("Local mounttable (%v) published as %q", mtName, n.Name)
	}
	return mtName, stopMT, err
}

// startDeviceServer creates an ipc.Server and sets it up to server the Device service.
//
// ls: ListenSpec for the server
// configState: configuration for the Device service dispatcher
// mt: Object address of the mounttable
// dm: Name to publish the device service under
// testMode: whether the service is to be run in test mode
// restarted: callback invoked when the device manager is restarted.
//
// Returns:
// (1) Function to be called to force the service to shutdown
// (2) Any errors in starting the service (in which case, (1) will be nil)
func startDeviceServer(ctx *context.T, args DeviceArgs, mt string) (shutdown func(), err error) {
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		return nil, err
	}
	shutdown = func() { server.Stop() }
	endpoints, err := server.Listen(args.ListenSpec)
	if err != nil {
		shutdown()
		return nil, err
	}
	args.ConfigState.Name = endpoints[0].Name()
	vlog.Infof("Device manager (%v) published as %v", args.ConfigState.Name, args.name(mt))

	dispatcher, err := impl.NewDispatcher(ctx, args.ConfigState, mt, args.TestMode, args.RestartCallback)
	if err != nil {
		shutdown()
		return nil, err
	}

	shutdown = func() {
		server.Stop()
		impl.Shutdown(dispatcher)
	}
	if err := server.ServeDispatcher(args.name(mt), dispatcher); err != nil {
		shutdown()
		return nil, err
	}
	return shutdown, nil
}

func mountGlobalNamespaceInLocalNamespace(ctx *context.T, localMT string) {
	ns := veyron2.GetNamespace(ctx)
	for _, root := range ns.Roots() {
		go func(r string) {
			for {
				err := ns.Mount(ctx, naming.Join(localMT, "global"), r, 0 /* forever */, naming.ServesMountTableOpt(true))
				if err == nil {
					break
				}
				vlog.Infof("Failed to Mount global namespace: %v", err)
				time.Sleep(time.Second)
			}
		}(root)
	}
}
