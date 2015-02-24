package impl

import (
	"v.io/v23/ipc"
	svtrace "v.io/v23/services/mgmt/vtrace"
	"v.io/v23/uniqueid"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
)

type vtraceService struct{}

func (v *vtraceService) Trace(ctx ipc.ServerContext, id uniqueid.Id) (vtrace.TraceRecord, error) {
	store := vtrace.GetStore(ctx.Context())
	tr := store.TraceRecord(id)
	if tr == nil {
		return vtrace.TraceRecord{}, verror.New(verror.ErrNoExist, ctx.Context(), "No trace with id %x", id)
	}
	return *tr, nil
}

func (v *vtraceService) AllTraces(ctx svtrace.StoreAllTracesContext) error {
	// TODO(mattr): Consider changing the store to allow us to iterate through traces
	// when there are many.
	store := vtrace.GetStore(ctx.Context())
	traces := store.TraceRecords()
	for i := range traces {
		if err := ctx.SendStream().Send(traces[i]); err != nil {
			return err
		}
	}
	return nil
}

func NewVtraceService() interface{} {
	return svtrace.StoreServer(&vtraceService{})
}
