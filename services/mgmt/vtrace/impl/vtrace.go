package impl

import (
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/uniqueid"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vtrace"
)

type vtraceService struct {
	store vtrace.Store
}

func (v *vtraceService) Trace(ctx ipc.ServerContext, id uniqueid.ID) (vtrace.TraceRecord, error) {
	tr := v.store.TraceRecord(id)
	if tr == nil {
		return vtrace.TraceRecord{}, verror2.Make(verror2.NoExist, ctx, "No trace with id %x", id)
	}
	return *tr, nil
}

// TODO(toddw): Change ipc.ServerCall into a struct stub context.
func (v *vtraceService) AllTraces(call ipc.ServerCall) error {
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

func NewVtraceService(store vtrace.Store) interface{} {
	return &vtraceService{store}
}
