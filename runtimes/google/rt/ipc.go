package rt

import (
	"fmt"

	iipc "veyron/runtimes/google/ipc"
	imanager "veyron/runtimes/google/ipc/stream/manager"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
)

func (rt *vrt) NewClient(opts ...ipc.ClientOpt) (ipc.Client, error) {
	sm := rt.sm
	ns := rt.ns
	cIDOpt := veyron2.LocalID(rt.id.Identity())
	var otherOpts []ipc.ClientOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.StreamManagerOpt:
			sm = topt.Manager
		case veyron2.NamespaceOpt:
			ns = topt.Namespace
		case veyron2.LocalIDOpt:
			cIDOpt = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	if cIDOpt.PrivateID != nil {
		otherOpts = append(otherOpts, cIDOpt)
	}
	return iipc.InternalNewClient(sm, ns, otherOpts...)
}

func (rt *vrt) Client() ipc.Client {
	return rt.client
}

func (rt *vrt) NewContext() context.T {
	return iipc.InternalNewContext()
}

func (rt *vrt) TODOContext() context.T {
	return iipc.InternalNewContext()
}

func (rt *vrt) NewServer(opts ...ipc.ServerOpt) (ipc.Server, error) {
	var err error

	// Create a new RoutingID (and StreamManager) for each server.
	// Except, in the common case of a process having a single Server,
	// use the same RoutingID (and StreamManager) that is used for Clients.
	rt.mu.Lock()
	sm := rt.sm
	rt.nServers++
	if rt.nServers > 1 {
		sm, err = rt.NewStreamManager()
	}
	rt.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to create ipc/stream/Manager: %v", err)
	}
	// Start the http debug server exactly once for this runtime.
	rt.startHTTPDebugServerOnce()
	ns := rt.ns
	var otherOpts []ipc.ServerOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.NamespaceOpt:
			ns = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the identity currently used by the runtime.
	otherOpts = append(otherOpts, rt.id)

	ctx := rt.NewContext()
	return iipc.InternalNewServer(ctx, sm, ns, otherOpts...)
}

func (rt *vrt) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	sm := imanager.InternalNew(rid)
	rt.debug.RegisterStreamManager(rid, sm)
	return sm, nil
}
