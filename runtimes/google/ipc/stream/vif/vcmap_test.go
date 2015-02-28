package vif

import (
	"reflect"
	"testing"

	"v.io/x/ref/runtimes/google/ipc/stream/vc"
)

func TestVCMap(t *testing.T) {
	m := newVCMap()

	vc12 := vc.InternalNew(vc.Params{VCI: 12})
	vc34 := vc.InternalNew(vc.Params{VCI: 34})
	vc45 := vc.InternalNew(vc.Params{VCI: 45})

	if vc, _, _ := m.Find(12); vc != nil {
		t.Errorf("Unexpected VC found: %+v", vc)
	}
	if ok, _, _ := m.Insert(vc34); !ok {
		t.Errorf("Insert should have returned true on first insert")
	}
	if ok, _, _ := m.Insert(vc34); ok {
		t.Errorf("Insert should have returned false on second insert")
	}
	if ok, _, _ := m.Insert(vc12); !ok {
		t.Errorf("Insert should have returned true on first insert")
	}
	if ok, _, _ := m.Insert(vc45); !ok {
		t.Errorf("Insert should have returned true on the first insert")
	}
	if g, w := m.List(), []*vc.VC{vc12, vc34, vc45}; !reflect.DeepEqual(g, w) {
		t.Errorf("Did not get all VCs in expected order. Got %v, want %v", g, w)
	}
	m.Delete(vc34.VCI())
	if g, w := m.List(), []*vc.VC{vc12, vc45}; !reflect.DeepEqual(g, w) {
		t.Errorf("Did not get all VCs in expected order. Got %v, want %v", g, w)
	}
}

func TestVCMapFreeze(t *testing.T) {
	m := newVCMap()
	vc1 := vc.InternalNew(vc.Params{VCI: 1})
	vc2 := vc.InternalNew(vc.Params{VCI: 2})
	if ok, _, _ := m.Insert(vc1); !ok {
		t.Fatal("Should be able to insert the VC")
	}
	m.Freeze()
	if ok, _, _ := m.Insert(vc2); ok {
		t.Errorf("Should not be able to insert a VC after Freeze")
	}
	if vc, _, _ := m.Find(1); vc != vc1 {
		t.Errorf("Got %v want %v", vc, vc1)
	}
	m.Delete(vc1.VCI())
	if vc, _, _ := m.Find(1); vc != nil {
		t.Errorf("Got %v want nil", vc)
	}
}
