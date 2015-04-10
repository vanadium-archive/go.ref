// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/verror"

	"v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/mgmt"
)

const pkgPath = "v.io/x/ref/option/internal/rt"

var (
	errConfigKeyNotSet = verror.Register(pkgPath+".errConfigKeyNotSet", verror.NoRetry, "{1:}{2:} {3} is not set{:_}")
)

func (rt *Runtime) initMgmt(ctx *context.T) error {
	handle, err := exec.GetChildHandle()
	if err == nil {
		// No error; fall through.
	} else if verror.ErrorID(err) == exec.ErrNoVersion.ID {
		// Do not initialize the mgmt runtime if the process has not
		// been started through the vanadium exec library by a device
		// manager.
		return nil
	} else {
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
	listenSpec, err := getListenSpec(ctx, handle)
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

func getListenSpec(ctx *context.T, handle *exec.ChildHandle) (*rpc.ListenSpec, error) {
	protocol, err := handle.Config.Get(mgmt.ProtocolConfigKey)
	if err != nil {
		return nil, err
	}
	if protocol == "" {
		return nil, verror.New(errConfigKeyNotSet, ctx, mgmt.ProtocolConfigKey)
	}

	address, err := handle.Config.Get(mgmt.AddressConfigKey)
	if err != nil {
		return nil, err
	}
	if address == "" {
		return nil, verror.New(errConfigKeyNotSet, ctx, mgmt.AddressConfigKey)
	}
	return &rpc.ListenSpec{Addrs: rpc.ListenAddrs{{protocol, address}}}, nil
}

func (rt *Runtime) callbackToParent(ctx *context.T, parentName, myName string) error {
	ctx, _ = context.WithTimeout(ctx, time.Minute)
	return rt.GetClient(ctx).Call(ctx, parentName, "Set", []interface{}{mgmt.AppCycleManagerConfigKey, myName}, nil)
}
