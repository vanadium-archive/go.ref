package ipc

import (
	"reflect"
	"testing"

	_ "veyron/lib/testutil"

	"veyron2/ipc"
	"veyron2/security"
)

type constDisp int

func (constDisp) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) { return nil, nil, nil }

const (
	dispDefault constDisp = iota
	dispa
	dispb
	dispab1
	dispab2
	dispabc
)

func register(t *testing.T, dt *disptrie, prefix string, disp ipc.Dispatcher) {
	if err := dt.Register(prefix, disp); err != nil {
		t.Errorf("dt.Register(%q, %v) error: %v", prefix, disp, err)
	}
}

func TestDisptrie(t *testing.T) {
	dt := newDisptrie()
	register(t, dt, "", dispDefault)
	register(t, dt, "a", dispa)
	register(t, dt, "b", dispb)
	register(t, dt, "a/b", dispab1)
	register(t, dt, "a/b/c", dispabc)
	register(t, dt, "a/b", dispab2)

	type D []ipc.Dispatcher
	tests := []struct {
		prefix       string
		expectSuffix string
		expectDisps  D
	}{
		{"", "", D{dispDefault}},
		{"c", "c", D{dispDefault}},
		{"c/0", "c/0", D{dispDefault}},
		{"z", "z", D{dispDefault}},
		{"z/0", "z/0", D{dispDefault}},

		{"a", "", D{dispa}},
		{"a/z", "z", D{dispa}},
		{"a/z/0", "z/0", D{dispa}},
		{"a/boo", "boo", D{dispa}},
		{"a/boo/0", "boo/0", D{dispa}},

		{"b", "", D{dispb}},
		{"b/z", "z", D{dispb}},
		{"b/z/0", "z/0", D{dispb}},
		{"b/coo", "coo", D{dispb}},
		{"b/coo/0", "coo/0", D{dispb}},

		{"a/b", "", D{dispab1, dispab2}},
		{"a/b/z", "z", D{dispab1, dispab2}},
		{"a/b/z/0", "z/0", D{dispab1, dispab2}},
		{"a/b/coo", "coo", D{dispab1, dispab2}},
		{"a/b/coo/0", "coo/0", D{dispab1, dispab2}},

		{"a/b/c", "", D{dispabc}},
		{"a/b/c/z", "z", D{dispabc}},
		{"a/b/c/z/0", "z/0", D{dispabc}},
		{"a/b/c/doo", "doo", D{dispabc}},
		{"a/b/c/doo/0", "doo/0", D{dispabc}},
	}
	for _, test := range tests {
		disps, suffix := dt.Lookup(test.prefix)
		if suffix != test.expectSuffix || !reflect.DeepEqual(D(disps), test.expectDisps) {
			t.Errorf("dt.Lookup(%q) got (%s, %v), want (%s, %v)", test.prefix, suffix, disps, test.expectSuffix, test.expectDisps)
		}
	}
}

func TestDisptrieStop(t *testing.T) {
	dt := newDisptrie()
	register(t, dt, "", dispDefault)
	dt.Stop()
	if expect, got := errRegisterOnStoppedDisptrie, dt.Register("b", dispa); got != expect {
		t.Errorf("expected error %v, got %v instead", expect, got)
	}
}
