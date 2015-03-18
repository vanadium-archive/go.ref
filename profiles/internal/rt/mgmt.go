package rt

import (
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/mgmt"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"

	"v.io/x/ref/lib/exec"
)

func (rt *Runtime) initMgmt(ctx *context.T) error {
	handle, err := exec.GetChildHandle()
	if err == exec.ErrNoVersion {
		// Do not initialize the mgmt runtime if the process has not
		// been started through the vanadium exec library by a device
		// manager.
		return nil
	} else if err != nil {
		return err
	}
	parentName, err := handle.Config.Get(mgmt.ParentNameConfigKey)
	if err != nil {
		// If the ParentNameConfigKey is not set, then this process has
		// not been started by the device manager, but the parent process
		// is still a Vanadium process using the exec library so we
		// call SetReady to let it know that this child process has
		// successfully started.
		return handle.SetReady()
	}
	listenSpec, err := getListenSpec(handle)
	if err != nil {
		return err
	}
	var serverOpts []rpc.ServerOpt
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
	if err := server.Serve("", v23.GetAppCycle(ctx).Remote(), nil); err != nil {
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

func getListenSpec(handle *exec.ChildHandle) (*rpc.ListenSpec, error) {
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
	return &rpc.ListenSpec{Addrs: rpc.ListenAddrs{{protocol, address}}}, nil
}

func (rt *Runtime) callbackToParent(ctx *context.T, parentName, myName string) error {
	ctx, _ = context.WithTimeout(ctx, time.Minute)
	call, err := rt.GetClient(ctx).StartCall(ctx, parentName, "Set", []interface{}{mgmt.AppCycleManagerConfigKey, myName})
	if err != nil {
		return err
	}
	return call.Finish()
}
