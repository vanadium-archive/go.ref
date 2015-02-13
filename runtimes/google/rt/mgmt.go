package rt

import (
	"fmt"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"

	"v.io/core/veyron/lib/exec"
)

func (rt *Runtime) initMgmt(ctx *context.T) error {
	handle, err := exec.GetChildHandle()
	if err == exec.ErrNoVersion {
		// Do not initialize the mgmt runtime if the process has not
		// been started through the veyron exec library by a device
		// manager.
		return nil
	} else if err != nil {
		return err
	}

	parentName, err := handle.Config.Get(mgmt.ParentNameConfigKey)
	if err != nil {
		return nil
	}
	listenSpec, err := getListenSpec(handle)
	if err != nil {
		return err
	}
	var serverOpts []ipc.ServerOpt
	parentPeerPattern, err := handle.Config.Get(mgmt.ParentBlessingConfigKey)
	if err == nil && parentPeerPattern != "" {
		// Grab the blessing from our blessing store that the parent
		// told us to use so they can talk to us.
		serverBlessing := rt.GetPrincipal(ctx).BlessingStore().ForPeer(parentPeerPattern)
		serverOpts = append(serverOpts, options.ServerBlessings{serverBlessing})
	}
	server, err := rt.NewServer(ctx, serverOpts...)
	if err != nil {
		return err
	}
	eps, err := server.Listen(*listenSpec)
	if err != nil {
		return err
	}
	if err := server.Serve("", veyron2.GetAppCycle(ctx).Remote(), nil); err != nil {
		server.Stop()
		return err
	}
	err = rt.callbackToParent(ctx, parentName, naming.JoinAddressName(eps[0].String(), ""))
	if err != nil {
		server.Stop()
		return err
	}

	return handle.SetReady()
}

func getListenSpec(handle *exec.ChildHandle) (*ipc.ListenSpec, error) {
	protocol, err := handle.Config.Get(mgmt.ProtocolConfigKey)
	if err != nil {
		return nil, err
	}
	if protocol == "" {
		return nil, fmt.Errorf("%v is not set", mgmt.ProtocolConfigKey)
	}

	address, err := handle.Config.Get(mgmt.AddressConfigKey)
	if err != nil {
		return nil, err
	}
	if address == "" {
		return nil, fmt.Errorf("%v is not set", mgmt.AddressConfigKey)
	}
	return &ipc.ListenSpec{Addrs: ipc.ListenAddrs{{protocol, address}}}, nil
}

func (rt *Runtime) callbackToParent(ctx *context.T, parentName, myName string) error {
	ctx, _ = context.WithTimeout(ctx, time.Minute)
	call, err := rt.GetClient(ctx).StartCall(ctx, parentName, "Set", []interface{}{mgmt.AppCycleManagerConfigKey, myName})

	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}
