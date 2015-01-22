package ipc

import (
	"sync"

	"v.io/core/veyron/runtimes/google/ipc/stream/vc"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"
)

// discharger implements vc.DischargeClient.
type dischargeClient struct {
	c          ipc.Client
	defaultCtx *context.T
	cache      dischargeCache
}

// InternalNewDischargeClient creates a vc.DischargeClient that will be used to
// fetch discharges to support blessings presented to a remote process.
//
// defaultCtx is the context used when none (nil) is explicitly provided to the
// PrepareDischarges call. This typically happens when fetching discharges on
// behalf of a server accepting connections, i.e., before any notion of the
// "context" of an API call has been established.
func InternalNewDischargeClient(defaultCtx *context.T, client ipc.Client) vc.DischargeClient {
	return &dischargeClient{
		c:          client,
		defaultCtx: defaultCtx,
		cache:      dischargeCache{cache: make(map[string]security.Discharge)},
	}
}

func (*dischargeClient) IPCStreamListenerOpt() {}
func (*dischargeClient) IPCStreamVCOpt()       {}

// PrepareDischarges retrieves the caveat discharges required for using blessings
// at server. The discharges are either found in the dischargeCache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an ipc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func (d *dischargeClient) PrepareDischarges(ctx *context.T, forcaveats []security.ThirdPartyCaveat, impetus security.DischargeImpetus) (ret []security.Discharge) {
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
	if d.cache.Discharges(caveats, discharges) > 0 {
		// Fetch discharges for caveats for which no discharges were found
		// in the cache.
		if ctx == nil {
			ctx = d.defaultCtx
		}
		if ctx != nil {
			var span vtrace.Span
			ctx, span = vtrace.SetNewSpan(ctx, "Fetching Discharges")
			defer span.Finish()
		}
		d.fetchDischarges(ctx, caveats, impetus, discharges)
	}
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
func (d *dischargeClient) fetchDischarges(ctx *context.T, caveats []security.ThirdPartyCaveat, impetus security.DischargeImpetus, out []security.Discharge) {
	var wg sync.WaitGroup
	for {
		type fetched struct {
			idx       int
			discharge security.Discharge
		}
		discharges := make(chan fetched, len(caveats))
		want := 0
		for i := range caveats {
			if out[i] != nil {
				continue
			}
			want++
			wg.Add(1)
			go func(i int, ctx *context.T, cav security.ThirdPartyCaveat) {
				defer wg.Done()
				vlog.VI(3).Infof("Fetching discharge for %v", cav)
				call, err := d.c.StartCall(ctx, cav.Location(), "Discharge", []interface{}{cav, filteredImpetus(cav.Requirements(), impetus)}, vc.NoDischarges{})
				if err != nil {
					vlog.VI(3).Infof("Discharge fetch for %v failed: %v", cav, err)
					return
				}
				var dAny vdl.AnyRep
				if ierr := call.Finish(&dAny, &err); ierr != nil || err != nil {
					vlog.VI(3).Infof("Discharge fetch for %v failed: (%v, %v)", cav, err, ierr)
					return
				}
				d, ok := dAny.(security.Discharge)
				if !ok {
					vlog.Errorf("fetchDischarges: server at %s sent a %T (%v) instead of a Discharge", cav.Location(), dAny, dAny)
				} else {
					vlog.VI(3).Infof("Fetched discharge for %v: %v", cav, d)
				}
				discharges <- fetched{i, d}
			}(i, ctx, caveats[i])
		}
		wg.Wait()
		close(discharges)
		var got int
		for fetched := range discharges {
			d.cache.Add(fetched.discharge)
			out[fetched.idx] = fetched.discharge
			got++
		}
		if want > 0 {
			vlog.VI(3).Infof("fetchDischarges: got %d of %d discharge(s) (total %d caveats)", got, want, len(caveats))
		}
		if got == 0 || got == want {
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
//
// REQUIRES: len(caveats) == len(out)
func (dcc *dischargeCache) Discharges(caveats []security.ThirdPartyCaveat, out []security.Discharge) (remaining int) {
	dcc.mu.Lock()
	for i, d := range out {
		if d != nil {
			continue
		}
		if out[i] = dcc.cache[caveats[i].ID()]; out[i] == nil {
			remaining++
		}
	}
	dcc.mu.Unlock()
	return
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
		after.Arguments = make([]vdl.AnyRep, len(before.Arguments))
		for i := range before.Arguments {
			after.Arguments[i] = before.Arguments[i]
		}
	}
	return
}
