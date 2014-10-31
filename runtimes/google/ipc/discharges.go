package ipc

import (
	"sync"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vlog"
)

func mkDischargeImpetus(serverBlessings []string, method string, args []interface{}) security.DischargeImpetus {
	var impetus security.DischargeImpetus
	if len(serverBlessings) > 0 {
		impetus.Server = make([]security.BlessingPattern, len(serverBlessings))
		for i, b := range serverBlessings {
			impetus.Server[i] = security.BlessingPattern(b)
		}
	}
	impetus.Method = method
	if len(args) > 0 {
		impetus.Arguments = make([]vdlutil.Any, len(args))
		for i, a := range args {
			impetus.Arguments[i] = vdlutil.Any(a)
		}
	}
	return impetus
}

// prepareDischarges retrieves the caveat discharges required for using blessings
// at server. The discharges are either found in the client cache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an ipc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func (c *client) prepareDischarges(ctx context.T, forcaveats []security.ThirdPartyCaveat, impetus security.DischargeImpetus, opts []ipc.CallOpt) (ret []security.Discharge) {
	// Make a copy since this copy will be mutated.
	var caveats []security.ThirdPartyCaveat
	for _, cav := range forcaveats {
		caveats = append(caveats, cav)
	}
	discharges := make([]security.Discharge, len(caveats))
	dischargesFromOpts(caveats, opts, discharges)
	c.dischargeCache.Discharges(caveats, discharges)
	if shouldFetchDischarges(opts) {
		c.fetchDischarges(ctx, caveats, impetus, opts, discharges)
	}
	for _, d := range discharges {
		if d != nil {
			ret = append(ret, d)
		}
	}
	return
}

// dischargeCache is a concurrency-safe cache for third party caveat discharges.
type dischargeCache struct {
	sync.RWMutex
	cache map[string]security.Discharge // GUARDED_BY(RWMutex)
}

// Add inserts the argument to the cache, possibly overwriting previous
// discharges for the same caveat.
func (dcc *dischargeCache) Add(discharges ...security.Discharge) {
	dcc.Lock()
	for _, d := range discharges {
		dcc.cache[d.ID()] = d
	}
	dcc.Unlock()
}

// Invalidate removes discharges from the cache.
func (dcc *dischargeCache) Invalidate(discharges ...security.Discharge) {
	dcc.Lock()
	for _, d := range discharges {
		if dcc.cache[d.ID()] == d {
			delete(dcc.cache, d.ID())
		}
	}
	dcc.Unlock()
}

// Discharges takes a slice of caveats and a slice of discharges of the same
// length and fills in nil entries in the discharges slice with discharges
// from the cache (if there are any).
// REQUIRES: len(caveats) == len(out)
func (dcc *dischargeCache) Discharges(caveats []security.ThirdPartyCaveat, out []security.Discharge) {
	dcc.Lock()
	for i, d := range out {
		if d != nil {
			continue
		}
		out[i] = dcc.cache[caveats[i].ID()]
	}
	dcc.Unlock()
}

// dischargesFromOpts fills in the nils in the out argument with discharges in
// opts that match the caveat at the same index in caveats.
// REQUIRES: len(caveats) == len(out)
func dischargesFromOpts(caveats []security.ThirdPartyCaveat, opts []ipc.CallOpt, out []security.Discharge) {
	for _, opt := range opts {
		d, ok := opt.(options.Discharge)
		if !ok {
			continue
		}
		for i, cav := range caveats {
			if out[i] == nil && d.ID() == cav.ID() {
				out[i] = d
			}
		}
	}
}

// fetchDischarges fills in out by fetching discharges for caveats from the
// appropriate discharge service. Since there may be dependencies in the
// caveats, fetchDischarges keeps retrying until either all discharges can be
// fetched or no new discharges are fetched.
// REQUIRES: len(caveats) == len(out)
func (c *client) fetchDischarges(ctx context.T, caveats []security.ThirdPartyCaveat, impetus security.DischargeImpetus, opts []ipc.CallOpt, out []security.Discharge) {
	opts = append([]ipc.CallOpt{dontFetchDischarges{}}, opts...)
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
				call, err := c.StartCall(ctx, cav.Location(), "Discharge", []interface{}{cav, filteredImpetus(cav.Requirements(), impetus)}, opts...)
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
			c.dischargeCache.Add(fetched.discharge)
			out[fetched.idx] = fetched.discharge
			got++
		}
		vlog.VI(2).Infof("fetchDischarges: got %d discharges", got)
		if got == 0 {
			return
		}
	}
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

// dontFetchDischares is an ipc.CallOpt that indicates that no extra ipc-s
// should be done to fetch discharges for the call with this opt.
// Discharges in the cache and in the call options are still used.
type dontFetchDischarges struct{}

func (dontFetchDischarges) IPCCallOpt() {
	//nologcall
}

func shouldFetchDischarges(opts []ipc.CallOpt) bool {
	for _, opt := range opts {
		if _, ok := opt.(dontFetchDischarges); ok {
			return false
		}
	}
	return true
}
