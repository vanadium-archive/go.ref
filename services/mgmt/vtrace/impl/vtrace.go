package impl

import (
	"v.io/veyron/veyron2/ipc"
	svtrace "v.io/veyron/veyron2/services/mgmt/vtrace"
	"v.io/veyron/veyron2/uniqueid"
	"v.io/veyron/veyron2/verror2"
	"v.io/veyron/veyron2/vtrace"
)

type vtraceService struct {
	store vtrace.Store
}

func (v *vtraceService) Trace(ctx ipc.ServerContext, id uniqueid.ID) (vtrace.TraceRecord, error) {
	tr := v.store.TraceRecord(id)
	if tr == nil {
		return vtrace.TraceRecord{}, verror2.Make(verror2.NoExist, ctx.Context(), "No trace with id %x", id)
	}
	return *tr, nil
}

func (v *vtraceService) AllTraces(ctx svtrace.StoreAllTracesContext) error {
	// TODO(mattr): Consider changing the store to allow us to iterate through traces
	// when there are many.
	traces := v.store.TraceRecords()
	for i := range traces {
		if err := ctx.SendStream().Send(traces[i]); err != nil {
			return err
		}
	}
	return nil
}

func NewVtraceService(store vtrace.Store) interface{} {
	return svtrace.StoreServer(&vtraceService{store})
}
