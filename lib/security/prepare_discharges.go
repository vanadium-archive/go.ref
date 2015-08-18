// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package security

import (
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vtrace"
)

// If this is attached to the context, we will not fetch discharges.
// We use this to prevent ourselves from fetching discharges in the
// process of fetching discharges, thus creating an infinite loop.
type skipDischargesKey struct{}

// PrepareDischarges retrieves the caveat discharges required for using blessings
// at server. The discharges are either found in the dischargeCache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an rpc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func PrepareDischarges(
	ctx *context.T,
	blessings security.Blessings,
	impetus security.DischargeImpetus,
	expiryBuffer time.Duration) map[string]security.Discharge {
	tpCavs := blessings.ThirdPartyCaveats()
	if skip, _ := ctx.Value(skipDischargesKey{}).(bool); skip || len(tpCavs) == 0 {
		return nil
	}
	ctx = context.WithValue(ctx, skipDischargesKey{}, true)

	// Make a copy since this copy will be mutated.
	var caveats []security.Caveat
	var filteredImpetuses []security.DischargeImpetus
	for _, cav := range tpCavs {
		// It shouldn't happen, but in case there are non-third-party
		// caveats, drop them.
		if tp := cav.ThirdPartyDetails(); tp != nil {
			caveats = append(caveats, cav)
			filteredImpetuses = append(filteredImpetuses, filteredImpetus(tp.Requirements(), impetus))
		}
	}
	bstore := v23.GetPrincipal(ctx).BlessingStore()
	// Gather discharges from cache.
	discharges, rem := discharges(bstore, caveats, impetus)
	if rem > 0 {
		// Fetch discharges for caveats for which no discharges were
		// found in the cache.
		if ctx != nil {
			var span vtrace.Span
			ctx, span = vtrace.WithNewSpan(ctx, "Fetching Discharges")
			defer span.Finish()
		}
		fetchDischarges(ctx, caveats, filteredImpetuses, discharges, expiryBuffer)
	}
	ret := make(map[string]security.Discharge, len(discharges))
	for _, d := range discharges {
		if d.ID() != "" {
			ret[d.ID()] = d
		}
	}
	return ret
}

func discharges(bs security.BlessingStore, caveats []security.Caveat, imp security.DischargeImpetus) (out []security.Discharge, rem int) {
	out = make([]security.Discharge, len(caveats))
	for i := range caveats {
		out[i] = bs.Discharge(caveats[i], imp)
		if out[i].ID() == "" {
			rem++
		}
	}
	return
}

// fetchDischarges fills out by fetching discharges for caveats from the
// appropriate discharge service. Since there may be dependencies in the
// caveats, fetchDischarges keeps retrying until either all discharges can be
// fetched or no new discharges are fetched.
// REQUIRES: len(caveats) == len(out)
// REQUIRES: caveats[i].ThirdPartyDetails() != nil for 0 <= i < len(caveats)
func fetchDischarges(
	ctx *context.T,
	caveats []security.Caveat,
	impetuses []security.DischargeImpetus,
	out []security.Discharge,
	expiryBuffer time.Duration) {
	bstore := v23.GetPrincipal(ctx).BlessingStore()
	var wg sync.WaitGroup
	for {
		type fetched struct {
			idx       int
			discharge security.Discharge
			caveat    security.Caveat
			impetus   security.DischargeImpetus
		}
		discharges := make(chan fetched, len(caveats))
		want := 0
		for i := range caveats {
			if !shouldFetchDischarge(out[i], expiryBuffer) {
				continue
			}
			want++
			wg.Add(1)
			go func(i int, ctx *context.T, cav security.Caveat) {
				defer wg.Done()
				tp := cav.ThirdPartyDetails()
				var dis security.Discharge
				ctx.VI(3).Infof("Fetching discharge for %v", tp)
				if err := v23.GetClient(ctx).Call(ctx, tp.Location(), "Discharge",
					[]interface{}{cav, impetuses[i]}, []interface{}{&dis}); err != nil {
					ctx.VI(3).Infof("Discharge fetch for %v failed: %v", tp, err)
					return
				}
				discharges <- fetched{i, dis, caveats[i], impetuses[i]}
			}(i, ctx, caveats[i])
		}
		wg.Wait()
		close(discharges)
		var got int
		for fetched := range discharges {
			bstore.CacheDischarge(fetched.discharge, fetched.caveat, fetched.impetus)
			out[fetched.idx] = fetched.discharge
			got++
		}
		if want > 0 {
			ctx.VI(3).Infof("fetchDischarges: got %d of %d discharge(s) (total %d caveats)", got, want, len(caveats))
		}
		if got == 0 || got == want {
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
		after.Arguments = make([]*vdl.Value, len(before.Arguments))
		for i := range before.Arguments {
			after.Arguments[i] = vdl.CopyValue(before.Arguments[i])
		}
	}
	return
}

func shouldFetchDischarge(dis security.Discharge, expiryBuffer time.Duration) bool {
	if dis.ID() == "" {
		return true
	}
	expiry := dis.Expiry()
	if expiry.IsZero() {
		return false
	}
	return expiry.Before(time.Now().Add(expiryBuffer))
}
