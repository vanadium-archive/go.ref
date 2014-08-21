package ipc

import (
	"reflect"
	"testing"

	"veyron2/vdl/test_base"
)

func init() {
	registerInterface((*test_base.ServiceB)(nil))
}

func compareType(t *testing.T, method string, got, want interface{}, argKind string) {
	if gotT, wantT := reflect.TypeOf(got), reflect.TypeOf(want); gotT != wantT {
		t.Errorf("type mismatch in %q's %s argument: got %v , want %v ", method, argKind, gotT, wantT)
	}
}

func compareTypes(t *testing.T, method string, got, want []interface{}, argKind string) {
	if len(got) != len(want) {
		t.Errorf("mismatch in input arguments: got %v , want %v ", got, want)
	}
	for i, _ := range got {
		compareType(t, method, got[i], want[i], argKind)
	}
}

func TestGetter(t *testing.T) {
	iface := "veyron2/vdl/test_base/ServiceB"
	getter, err := newArgGetter([]string{iface})
	if err != nil {
		t.Fatalf("couldn't find getter for interface: %v ", iface)
	}
	data := []struct {
		Method       string
		NumInArgs    int
		in, out      []interface{}
		sSend, sRecv interface{}
		sFinish      []interface{}
	}{
		{"MethodA1", 0, nil, nil, nil, nil, nil},
		{"MethodA2", 2, []interface{}{(*int32)(nil), (*string)(nil)}, []interface{}{(*string)(nil)}, nil, nil, nil},
		{"MethodA3", 1, []interface{}{(*int32)(nil)}, nil, nil, (*test_base.Scalars)(nil), []interface{}{(*string)(nil)}},
		{"MethodA4", 1, []interface{}{(*int32)(nil)}, nil, (*int32)(nil), (*string)(nil), nil},
		{"MethodB1", 2, []interface{}{(*test_base.Scalars)(nil), (*test_base.Composites)(nil)}, []interface{}{(*test_base.CompComp)(nil)}, nil, nil, nil},
	}
	for _, d := range data {
		m := getter.FindMethod(d.Method, d.NumInArgs)
		if m == nil {
			t.Errorf("couldn't find method %q with %d args", d.Method, d.NumInArgs)
			continue
		}
		// Compare arguments.
		compareTypes(t, d.Method, m.InPtrs(), d.in, "input")
		compareTypes(t, d.Method, m.OutPtrs(), d.out, "output")
		compareType(t, d.Method, m.StreamSendPtr(), d.sSend, "stream send")
		compareType(t, d.Method, m.StreamRecvPtr(), d.sRecv, "stream recv")
		compareTypes(t, d.Method, m.StreamFinishPtrs(), d.sFinish, "stream finish")
	}
}
