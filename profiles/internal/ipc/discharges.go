package ipc

import (
	"sync"
	"time"

	"v.io/x/ref/profiles/internal/ipc/stream/vc"

	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
)

// NoDischarges specifies that the RPC call should not fetch discharges.
type NoDischarges struct{}

func (NoDischarges) IPCCallOpt()   {}
func (NoDischarges) NSResolveOpt() {}

// discharger implements vc.DischargeClient.
type dischargeClient struct {
	c                     ipc.Client
	defaultCtx            *context.T
	cache                 dischargeCache
	dischargeExpiryBuffer time.Duration
}

// InternalNewDischargeClient creates a vc.DischargeClient that will be used to
// fetch discharges to support blessings presented to a remote process.
//
// defaultCtx is the context used when none (nil) is explicitly provided to the
// PrepareDischarges call. This typically happens when fetching discharges on
// behalf of a server accepting connections, i.e., before any notion of the
// "context" of an API call has been established.
// dischargeExpiryBuffer specifies how much before discharge expiration we should
// refresh discharges.
// Attempts will be made to refresh a discharge DischargeExpiryBuffer before they expire.
func InternalNewDischargeClient(defaultCtx *context.T, client ipc.Client, dischargeExpiryBuffer time.Duration) vc.DischargeClient {
	return &dischargeClient{
		c:          client,
		defaultCtx: defaultCtx,
		cache:      dischargeCache{cache: make(map[string]security.Discharge)},
		dischargeExpiryBuffer: dischargeExpiryBuffer,
	}
}

func (*dischargeClient) IPCStreamListenerOpt() {}
func (*dischargeClient) IPCStreamVCOpt()       {}

// PrepareDischarges retrieves the caveat discharges required for using blessings
// at server. The discharges are either found in the dischargeCache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an ipc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func (d *dischargeClient) PrepareDischarges(ctx *context.T, forcaveats []security.Caveat, impetus security.DischargeImpetus) (ret []security.Discharge) {
	if len(forcaveats) == 0 {
		return
	}
	// Make a copy since this copy will be mutated.
	var caveats []security.Caveat
	for _, cav := range forcaveats {
		// It shouldn't happen, but in case there are non-third-party
		// caveats, drop them.
		if tp := cav.ThirdPartyDetails(); tp != nil {
			caveats = append(caveats, cav)
		}
	}

	// Gather discharges from cache.
	// (Collect a set of pointers, where nil implies a missing discharge)
	discharges := make([]*security.Discharge, len(caveats))
	if d.cache.Discharges(caveats, discharges) > 0 {
		// Fetch discharges for caveats for which no discharges were
		// found in the cache.
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
			ret = append(ret, *d)
		}
	}
	return
}
func (d *dischargeClient) Invalidate(discharges ...security.Discharge) {
	d.cache.invalidate(discharges...)
}

// fetchDischarges fills out by fetching discharges for caveats from the
// appropriate discharge service. Since there may be dependencies in the
// caveats, fetchDischarges keeps retrying until either all discharges can be
// fetched or no new discharges are fetched.
// REQUIRES: len(caveats) == len(out)
// REQUIRES: caveats[i].ThirdPartyDetails() != nil for 0 <= i < len(caveats)
func (d *dischargeClient) fetchDischarges(ctx *context.T, caveats []security.Caveat, impetus security.DischargeImpetus, out []*security.Discharge) {
	var wg sync.WaitGroup
	for {
		type fetched struct {
			idx       int
			discharge *security.Discharge
		}
		discharges := make(chan fetched, len(caveats))
		want := 0
		for i := range caveats {
			if !d.shouldFetchDischarge(out[i]) {
				continue
			}
			want++
			wg.Add(1)
			go func(i int, ctx *context.T, cav security.Caveat) {
				defer wg.Done()
				tp := cav.ThirdPartyDetails()
				vlog.VI(3).Infof("Fetching discharge for %v", tp)
				call, err := d.c.StartCall(ctx, tp.Location(), "Discharge", []interface{}{cav, filteredImpetus(tp.Requirements(), impetus)}, NoDischarges{})
				if err != nil {
					vlog.VI(3).Infof("Discharge fetch for %v failed: %v", tp, err)
					return
				}
				var d security.Discharge
				if err := call.Finish(&d); err != nil {
					vlog.VI(3).Infof("Discharge fetch for %v failed: (%v)", cav, err)
					return
				}
				discharges <- fetched{i, &d}
			}(i, ctx, caveats[i])
		}
		wg.Wait()
		close(discharges)
		var got int
		for fetched := range discharges {
			d.cache.Add(*fetched.discharge)
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

func (d *dischargeClient) shouldFetchDischarge(dis *security.Discharge) bool {
	if dis == nil {
		return true
	}
	expiry := dis.Expiry()
	if expiry.IsZero() {
		return false
	}
	return expiry.Before(time.Now().Add(d.dischargeExpiryBuffer))
}

// dischargeCache is a concurrency-safe cache for third party caveat discharges.
// TODO(suharshs,ataly,ashankar): This should be keyed by filtered impetus as well.
type dischargeCache struct {
	mu    sync.RWMutex
	cache map[string]security.Discharge // GUARDED_BY(mu)
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
// length and fills in nil entries in the discharges slice with pointers to
// cached discharges (if there are any).
//
// REQUIRES: len(caveats) == len(out)
// REQUIRES: caveats[i].ThirdPartyDetails() != nil, for all 0 <= i < len(caveats)
func (dcc *dischargeCache) Discharges(caveats []security.Caveat, out []*security.Discharge) (remaining int) {
	dcc.mu.Lock()
	for i, d := range out {
		if d != nil {
			continue
		}
		if cached, exists := dcc.cache[caveats[i].ThirdPartyDetails().ID()]; exists {
			out[i] = &cached
			// If the discharge has expired, purge it from the cache.
			if hasDischargeExpired(out[i]) {
				out[i] = nil
				delete(dcc.cache, cached.ID())
				remaining++
			}
		} else {
			remaining++
		}
	}
	dcc.mu.Unlock()
	return
}

func hasDischargeExpired(dis *security.Discharge) bool {
	expiry := dis.Expiry()
	if expiry.IsZero() {
		return false
	}
	return expiry.Before(time.Now())
}

func (dcc *dischargeCache) invalidate(discharges ...security.Discharge) {
	dcc.mu.Lock()
	for _, d := range discharges {
		// TODO(ashankar,ataly): The cached discharge might have been
		// replaced by the time invalidate is called.
		// Should we have an "Equals" function defined on "Discharge"
		// and use that? (Could use reflect.DeepEqual as well, but
		// that will likely be expensive)
		// if cached := dcc.cache[d.ID()]; cached.Equals(d) {
		// 	delete(dcc.cache, d.ID())
		// }
		delete(dcc.cache, d.ID())
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
		after.Arguments = make([]*vdl.Value, len(before.Arguments))
		for i := range before.Arguments {
			after.Arguments[i] = vdl.CopyValue(before.Arguments[i])
		}
	}
	return
}
