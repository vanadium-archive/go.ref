// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"sync"
	"time"

	"v.io/x/ref/lib/apilog"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vtrace"
)

// NoDischarges specifies that the RPC call should not fetch discharges.
type NoDischarges struct{}

func (NoDischarges) RPCCallOpt() {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
}
func (NoDischarges) NSOpt() {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
}

// discharger implements vc.DischargeClient.
type dischargeClient struct {
	c                     rpc.Client
	defaultCtx            *context.T
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
func InternalNewDischargeClient(defaultCtx *context.T, client rpc.Client, dischargeExpiryBuffer time.Duration) vc.DischargeClient {
	return &dischargeClient{
		c:                     client,
		defaultCtx:            defaultCtx,
		dischargeExpiryBuffer: dischargeExpiryBuffer,
	}
}

func (*dischargeClient) RPCStreamListenerOpt() {}
func (*dischargeClient) RPCStreamVCOpt()       {}

// PrepareDischarges retrieves the caveat discharges required for using blessings
// at server. The discharges are either found in the dischargeCache, in the call
// options, or requested from the discharge issuer indicated on the caveat.
// Note that requesting a discharge is an rpc call, so one copy of this
// function must be able to successfully terminate while another is blocked.
func (d *dischargeClient) PrepareDischarges(ctx *context.T, forcaveats []security.Caveat, impetus security.DischargeImpetus) (ret []security.Discharge) {
	if len(forcaveats) == 0 {
		return
	}
	// Make a copy since this copy will be mutated.
	var caveats []security.Caveat
	var filteredImpetuses []security.DischargeImpetus
	for _, cav := range forcaveats {
		// It shouldn't happen, but in case there are non-third-party
		// caveats, drop them.
		if tp := cav.ThirdPartyDetails(); tp != nil {
			caveats = append(caveats, cav)
			filteredImpetuses = append(filteredImpetuses, filteredImpetus(tp.Requirements(), impetus))
		}
	}

	if ctx == nil {
		ctx = d.defaultCtx
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
		d.fetchDischarges(ctx, caveats, filteredImpetuses, discharges)
	}
	for _, d := range discharges {
		if d.ID() != "" {
			ret = append(ret, d)
		}
	}
	return
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

func (d *dischargeClient) Invalidate(ctx *context.T, discharges ...security.Discharge) {
	if ctx == nil {
		ctx = d.defaultCtx
	}
	bstore := v23.GetPrincipal(ctx).BlessingStore()
	bstore.ClearDischarges(discharges...)
}

// fetchDischarges fills out by fetching discharges for caveats from the
// appropriate discharge service. Since there may be dependencies in the
// caveats, fetchDischarges keeps retrying until either all discharges can be
// fetched or no new discharges are fetched.
// REQUIRES: len(caveats) == len(out)
// REQUIRES: caveats[i].ThirdPartyDetails() != nil for 0 <= i < len(caveats)
func (d *dischargeClient) fetchDischarges(ctx *context.T, caveats []security.Caveat, impetuses []security.DischargeImpetus, out []security.Discharge) {
	bstore := v23.GetPrincipal(ctx).BlessingStore()
	for {
		type fetched struct {
			idx       int
			discharge security.Discharge
			caveat    security.Caveat
			impetus   security.DischargeImpetus
		}
		discharges := make(chan fetched, len(caveats))
		want := 0
		var wg sync.WaitGroup
		for i := range caveats {
			if !d.shouldFetchDischarge(out[i]) {
				continue
			}
			want++
			wg.Add(1)
			go func(i int, ctx *context.T, cav security.Caveat) {
				defer wg.Done()
				tp := cav.ThirdPartyDetails()
				var dis security.Discharge
				ctx.VI(3).Infof("Fetching discharge for %v", tp)
				if err := d.c.Call(ctx, tp.Location(), "Discharge", []interface{}{cav, impetuses[i]}, []interface{}{&dis}, NoDischarges{}); err != nil {
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

func (d *dischargeClient) shouldFetchDischarge(dis security.Discharge) bool {
	if dis.ID() == "" {
		return true
	}
	expiry := dis.Expiry()
	if expiry.IsZero() {
		return false
	}
	return expiry.Before(time.Now().Add(d.dischargeExpiryBuffer))
}
