package rt

import (
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
	mt := rt.mt
	cIDOpt := veyron2.LocalID(rt.id.Identity())
	var otherOpts []ipc.ClientOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.StreamManagerOpt:
			sm = topt.Manager
		case veyron2.MountTableOpt:
			mt = topt.MountTable
		case veyron2.LocalIDOpt:
			cIDOpt = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	if cIDOpt.PrivateID != nil {
		otherOpts = append(otherOpts, cIDOpt)
	}
	return iipc.InternalNewClient(sm, mt, otherOpts...)
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
	// Start the http debug server exactly once for this process
	rt.startHTTPDebugServerOnce()

	sm := rt.sm
	mt := rt.mt
	var otherOpts []ipc.ServerOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.StreamManagerOpt:
			sm = topt
		case veyron2.MountTableOpt:
			mt = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the identity currently used by the runtime.
	otherOpts = append(otherOpts, rt.id)

	ctx := rt.NewContext()
	return iipc.InternalNewServer(ctx, sm, mt, otherOpts...)
}

func (rt *vrt) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	rt.rid = rid
	return imanager.InternalNew(rt.rid), nil
}

func (rt *vrt) StreamManager() stream.Manager {
	return rt.sm
}
