package impl

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/uniqueid"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vtrace"
)

type vtraceServer struct {
	store vtrace.Store
}

func (v *vtraceServer) Trace(call ipc.ServerCall, id uniqueid.ID) (vtrace.TraceRecord, error) {
	tr := v.store.TraceRecord(id)
	if tr == nil {
		return vtrace.TraceRecord{}, verror2.Make(verror2.NoExist, call, "No trace with id %x", id)
	}
	return *tr, nil
}

func (v *vtraceServer) AllTraces(call ipc.ServerCall) error {
	// TODO(mattr): Consider changing the store to allow us to iterate through traces
	// when there are many.
	traces := v.store.TraceRecords()
	for i := range traces {
		if err := call.Send(traces[i]); err != nil {
			return err
		}
	}
	return nil
}

func NewVtraceInvoker(store vtrace.Store) ipc.Invoker {
	return ipc.ReflectInvoker(&vtraceServer{store})
}

func NewVtraceService(store vtrace.Store) interface{} {
	return &vtraceServer{store}
}
