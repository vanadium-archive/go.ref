package ipc

import (
	"sync"
	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/security"
	"veyron2/vdl/vdlutil"
	"veyron2/vlog"
)

// prepareDischarges retrieves the caveat discharges required for using blessing
// at server. The discharges are either found in the client cache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an ipc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func (c *client) prepareDischarges(ctx context.T, blessing, server security.PublicID,
	method string, opts []ipc.CallOpt) (ret []security.ThirdPartyDischarge) {
	// TODO(andreser,ataly): figure out whether this should return an error and how that should be handled
	// Missing discharges do not necessarily mean the blessing is invalid (e.g., SetID)
	if blessing == nil {
		return
	}

	var caveats []security.ThirdPartyCaveat
	for _, cav := range blessing.ThirdPartyCaveats() {
		if server.Match(cav.Service) {
			caveats = append(caveats, cav.Caveat.(security.ThirdPartyCaveat))
		}
	}
	if len(caveats) == 0 {
		return
	}

	discharges := make([]security.ThirdPartyDischarge, len(caveats))
	dischargesFromOpts(caveats, opts, discharges)
	c.dischargeCache.Discharges(caveats, discharges)
	if shouldFetchDischarges(opts) {
		c.fetchDischarges(ctx, caveats, opts, discharges)
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
	security.CaveatDischargeMap // GUARDED_BY(RWMutex)
}

// Add inserts the argument to the cache, possibly overwriting previous
// discharges for the same caveat.
func (dcc *dischargeCache) Add(discharges ...security.ThirdPartyDischarge) {
	dcc.Lock()
	for _, d := range discharges {
		dcc.CaveatDischargeMap[d.CaveatID()] = d
	}
	dcc.Unlock()
}

// Invalidate removes discharges from the cache.
func (dcc *dischargeCache) Invalidate(discharges ...security.ThirdPartyDischarge) {
	dcc.Lock()
	for _, d := range discharges {
		if dcc.CaveatDischargeMap[d.CaveatID()] == d {
			delete(dcc.CaveatDischargeMap, d.CaveatID())
		}
	}
	dcc.Unlock()
}

// Discharges takes a slice of caveats and a slice of discharges of the same
// length and fills in nil entries in the discharges slice with discharges
// from the cache (if there are any).
// REQUIRES: len(caveats) == len(out)
func (dcc *dischargeCache) Discharges(caveats []security.ThirdPartyCaveat, out []security.ThirdPartyDischarge) {
	dcc.Lock()
	for i, d := range out {
		if d != nil {
			continue
		}
		out[i] = dcc.CaveatDischargeMap[caveats[i].ID()]
	}
	dcc.Unlock()
}

// dischargesFromOpts fills in the nils in the out argument with discharges in
// opts that match the caveat at the same index in caveats.
// REQUIRES: len(caveats) == len(out)
func dischargesFromOpts(caveats []security.ThirdPartyCaveat, opts []ipc.CallOpt,
	out []security.ThirdPartyDischarge) {
	for _, opt := range opts {
		d, ok := opt.(veyron2.DischargeOpt)
		if !ok {
			continue
		}
		for i, cav := range caveats {
			if out[i] == nil && d.CaveatID() == cav.ID() {
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
func (c *client) fetchDischarges(ctx context.T, caveats []security.ThirdPartyCaveat, opts []ipc.CallOpt, out []security.ThirdPartyDischarge) {
	opts = append([]ipc.CallOpt{dontFetchDischarges{}}, opts...)
	var wg sync.WaitGroup
	for {
		type fetched struct {
			idx       int
			discharge security.ThirdPartyDischarge
		}
		discharges := make(chan fetched, len(caveats))
		for i := range caveats {
			if out[i] != nil {
				continue
			}
			wg.Add(1)
			go func(i int, cav security.ThirdPartyCaveat) {
				defer wg.Done()
				vlog.VI(3).Infof("Fetching discharge for %T from %v", cav, cav.Location())
				call, err := c.StartCall(ctx, cav.Location(), "Discharge", []interface{}{cav}, opts...)
				if err != nil {
					vlog.VI(3).Infof("Discharge fetch for caveat %T from %v failed: %v", cav, cav.Location(), err)
					return
				}
				var dAny vdlutil.Any
				// TODO(ashankar): Retry on errors like no-route-to-service, name resolution failures etc.
				ierr := call.Finish(&dAny, &err)
				if ierr != nil || err != nil {
					vlog.VI(3).Infof("Discharge fetch for caveat %T from %v failed: (%v, %v)", cav, cav.Location(), err, ierr)
					return
				}
				d, ok := dAny.(security.ThirdPartyDischarge)
				if !ok {
					vlog.Errorf("fetchDischarges: server at %s sent a %T (%v) instead of a ThirdPartyDischarge", cav.Location(), dAny, dAny)
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

// dontFetchDischares is an ipc.CallOpt that indicates that no extre ipc-s
// should be done to fetch discharges for the call with this opt.
// Discharges in the cache and in the call options are still used.
type dontFetchDischarges struct{}

func (dontFetchDischarges) IPCCallOpt() {}
func shouldFetchDischarges(opts []ipc.CallOpt) bool {
	for _, opt := range opts {
		if _, ok := opt.(dontFetchDischarges); ok {
			return false
		}
	}
	return true
}
