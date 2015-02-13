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
	mtName, stopMT, err := mounttable.StartServers(ctx, args.Namespace.ListenSpec, args.Namespace.Name, args.Namespace.Neighborhood, args.Namespace.ACLFile)
	if err != nil {
		vlog.Errorf("mounttable.StartServers(%#v) failed: %v", args.Namespace, err)
		return nil, err
	}
	vlog.Infof("Local mounttable (%v) published as %q", mtName, args.Namespace.Name)

	if args.Device.Name == "" {
		args.Device.Name = naming.Join(mtName, "devmgr")
	}
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
	vlog.Infof("Device manager object name: %v", args.ConfigState.Name)

	dispatcher, err := impl.NewDispatcher(ctx, args.ConfigState, mt, args.PairingToken, args.TestMode, args.RestartCallback)
	if err != nil {
		shutdown()
		return nil, err
	}

	shutdown = func() {
		server.Stop()
		impl.Shutdown(dispatcher)
	}
	if err := server.ServeDispatcher(args.Name, dispatcher); err != nil {
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
