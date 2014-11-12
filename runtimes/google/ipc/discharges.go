package ipc

import (
	"sync"

	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vlog"
)

// discharger implements vc.DischargeClient.
type dischargeClient struct {
	c     ipc.Client
	ctx   context.T
	cache dischargeCache
}

func InternalNewDischargeClient(streamMgr stream.Manager, ns naming.Namespace, ctx context.T, opts ...ipc.ClientOpt) (vc.DischargeClient, error) {
	c, err := InternalNewClient(streamMgr, ns, opts...)
	if err != nil {
		return nil, err
	}
	return &dischargeClient{
		c:     c,
		ctx:   ctx,
		cache: dischargeCache{cache: make(map[string]security.Discharge)},
	}, nil
}

func (*dischargeClient) IPCClientOpt()         {}
func (*dischargeClient) IPCServerOpt()         {}
func (*dischargeClient) IPCStreamListenerOpt() {}
func (*dischargeClient) IPCStreamVCOpt()       {}

// PrepareDischarges retrieves the caveat discharges required for using blessings
// at server. The discharges are either found in the dischargeCache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an ipc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func (d *dischargeClient) PrepareDischarges(forcaveats []security.ThirdPartyCaveat, impetus security.DischargeImpetus) (ret []security.Discharge) {
	if len(forcaveats) == 0 {
		return
	}
	// Make a copy since this copy will be mutated.
	var caveats []security.ThirdPartyCaveat
	for _, cav := range forcaveats {
		caveats = append(caveats, cav)
	}

	// Gather discharges from cache.
	discharges := make([]security.Discharge, len(caveats))
	d.cache.Discharges(caveats, discharges)

	// Fetch discharges for caveats for which no discharges was found
	// in the cache.
	d.fetchDischarges(d.ctx, caveats, impetus, discharges)
	for _, d := range discharges {
		if d != nil {
			ret = append(ret, d)
		}
	}
	return
}
func (d *dischargeClient) Invalidate(discharges ...security.Discharge) {
	d.cache.invalidate(discharges...)
}

// fetchDischarges fills in out by fetching discharges for caveats from the
// appropriate discharge service. Since there may be dependencies in the
// caveats, fetchDischarges keeps retrying until either all discharges can be
// fetched or no new discharges are fetched.
// REQUIRES: len(caveats) == len(out)
func (d *dischargeClient) fetchDischarges(ctx context.T, caveats []security.ThirdPartyCaveat, impetus security.DischargeImpetus, out []security.Discharge) {
	var wg sync.WaitGroup
	for {
		type fetched struct {
			idx       int
			discharge security.Discharge
		}
		discharges := make(chan fetched, len(caveats))
		for i := range caveats {
			if out[i] != nil {
				continue
			}
			wg.Add(1)
			go func(i int, cav security.ThirdPartyCaveat) {
				defer wg.Done()
				vlog.VI(3).Infof("Fetching discharge for %T from %v (%+v)", cav, cav.Location(), cav.Requirements())
				call, err := d.c.StartCall(ctx, cav.Location(), "Discharge", []interface{}{cav, filteredImpetus(cav.Requirements(), impetus)})
				if err != nil {
					vlog.VI(3).Infof("Discharge fetch for caveat %T from %v failed: %v", cav, cav.Location(), err)
					return
				}
				var dAny vdlutil.Any
				if ierr := call.Finish(&dAny, &err); ierr != nil || err != nil {
					vlog.VI(3).Infof("Discharge fetch for caveat %T from %v failed: (%v, %v)", cav, cav.Location(), err, ierr)
					return
				}
				d, ok := dAny.(security.Discharge)
				if !ok {
					vlog.Errorf("fetchDischarges: server at %s sent a %T (%v) instead of a Discharge", cav.Location(), dAny, dAny)
				}
				discharges <- fetched{i, d}
			}(i, caveats[i])
		}
		wg.Wait()
		close(discharges)
		var got int
		for fetched := range discharges {
			d.cache.Add(fetched.discharge)
			out[fetched.idx] = fetched.discharge
			got++
		}
		vlog.VI(2).Infof("fetchDischarges: got %d discharges", got)
		if got == 0 {
			return
		}
	}
}

// dischargeCache is a concurrency-safe cache for third party caveat discharges.
type dischargeCache struct {
	mu    sync.RWMutex
	cache map[string]security.Discharge // GUARDED_BY(RWMutex)
}

// Add inserts the argument to the cache, possibly overwriting previous
// discharges for the same caveat.
func (dcc *dischargeCache) Add(discharges ...security.Discharge) {
	dcc.mu.Lock()
	for _, d := range discharges {
		dcc.cache[d.ID()] = d
	}
	dcc.mu.Unlock()
}

// Discharges takes a slice of caveats and a slice of discharges of the same
// length and fills in nil entries in the discharges slice with discharges
// from the cache (if there are any).
// REQUIRES: len(caveats) == len(out)
func (dcc *dischargeCache) Discharges(caveats []security.ThirdPartyCaveat, out []security.Discharge) {
	dcc.mu.Lock()
	for i, d := range out {
		if d != nil {
			continue
		}
		out[i] = dcc.cache[caveats[i].ID()]
	}
	dcc.mu.Unlock()
}

func (dcc *dischargeCache) invalidate(discharges ...security.Discharge) {
	dcc.mu.Lock()
	for _, d := range discharges {
		if dcc.cache[d.ID()] == d {
			delete(dcc.cache, d.ID())
		}
	}
	dcc.mu.Unlock()
}

// filteredImpetus returns a copy of 'before' after removing any values that are not required as per 'r'.
func filteredImpetus(r security.ThirdPartyRequirements, before security.DischargeImpetus) (after security.DischargeImpetus) {
	if r.ReportServer && len(before.Server) > 0 {
		after.Server = make([]security.BlessingPattern, len(before.Server))
		for i := range before.Server {
			after.Server[i] = before.Server[i]
		}
	}
	if r.ReportMethod {
		after.Method = before.Method
	}
	if r.ReportArguments && len(before.Arguments) > 0 {
		after.Arguments = make([]vdlutil.Any, len(before.Arguments))
		for i := range before.Arguments {
			after.Arguments[i] = before.Arguments[i]
		}
	}
	return
}
