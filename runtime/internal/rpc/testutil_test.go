// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"reflect"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/flags"
	ivtrace "v.io/x/ref/runtime/internal/vtrace"
	"v.io/x/ref/test"
)

func makeResultPtrs(ins []interface{}) []interface{} {
	outs := make([]interface{}, len(ins))
	for ix, in := range ins {
		typ := reflect.TypeOf(in)
		if typ == nil {
			// Nil indicates interface{}.
			var empty interface{}
			typ = reflect.ValueOf(&empty).Elem().Type()
		}
		outs[ix] = reflect.New(typ).Interface()
	}
	return outs
}

func checkResultPtrs(t *testing.T, name string, gotptrs, want []interface{}) {
	for ix, res := range gotptrs {
		got := reflect.ValueOf(res).Elem().Interface()
		want := want[ix]
		switch g := got.(type) {
		case verror.E:
			w, ok := want.(verror.E)
			// don't use reflect deep equal on verror's since they contain
			// a list of stack PCs which will be different.
			if !ok {
				t.Errorf("%s result %d got type %T, want %T", name, ix, g, w)
			}
			if verror.ErrorID(g) != w.ID {
				t.Errorf("%s result %d got %v, want %v", name, ix, g, w)
			}
		default:
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s result %d got %v, want %v", name, ix, got, want)
			}
		}

	}
}

func mkCaveat(cav security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return cav
}

func bless(blesser, blessed security.Principal, extension string, caveats ...security.Caveat) security.Blessings {
	if len(caveats) == 0 {
		caveats = append(caveats, security.UnconstrainedUse())
	}
	b, err := blesser.Bless(blessed.PublicKey(), blesser.BlessingStore().Default(), extension, caveats[0], caveats[1:]...)
	if err != nil {
		panic(err)
	}
	return b
}

func initForTest() (*context.T, v23.Shutdown) {
	ctx, shutdown := test.V23Init()
	ctx, err := ivtrace.Init(ctx, flags.VtraceFlags{})
	if err != nil {
		panic(err)
	}
	ctx, _ = vtrace.WithNewTrace(ctx)
	return ctx, shutdown
}

func mkThirdPartyCaveat(discharger security.PublicKey, location string, c security.Caveat) security.Caveat {
	tpc, err := security.NewPublicKeyCaveat(discharger, location, security.ThirdPartyRequirements{}, c)
	if err != nil {
		panic(err)
	}
	return tpc
}

// mockCall implements security.Call
type mockCall struct {
	p        security.Principal
	l, r     security.Blessings
	m        string
	ld, rd   security.Discharge
	lep, rep naming.Endpoint
}

var _ security.Call = (*mockCall)(nil)

func (c *mockCall) Timestamp() (t time.Time) { return }
func (c *mockCall) Method() string           { return c.m }
func (c *mockCall) MethodTags() []*vdl.Value { return nil }
func (c *mockCall) Suffix() string           { return "" }
func (c *mockCall) LocalDischarges() map[string]security.Discharge {
	return map[string]security.Discharge{c.ld.ID(): c.ld}
}
func (c *mockCall) RemoteDischarges() map[string]security.Discharge {
	return map[string]security.Discharge{c.rd.ID(): c.rd}
}
func (c *mockCall) LocalEndpoint() naming.Endpoint      { return c.lep }
func (c *mockCall) RemoteEndpoint() naming.Endpoint     { return c.rep }
func (c *mockCall) LocalPrincipal() security.Principal  { return c.p }
func (c *mockCall) LocalBlessings() security.Blessings  { return c.l }
func (c *mockCall) RemoteBlessings() security.Blessings { return c.r }
