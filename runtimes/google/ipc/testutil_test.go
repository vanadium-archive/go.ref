package ipc

import (
	"reflect"
	"testing"

	_ "veyron/lib/testutil"
	"veyron/runtimes/google/ipc/stream/vc"
	isecurity "veyron/runtimes/google/security"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/verror"
)

func makeResultPtrs(ins []interface{}) []interface{} {
	outs := make([]interface{}, len(ins))
	for ix, in := range ins {
		typ := reflect.TypeOf(in)
		if typ == nil {
			// nil interfaces can only imply an error.
			// This is in sync with result2vom in server.go.  The
			// reasons for this check and conditions for when it
			// can be removed can be seen in the comments for
			// result2vom.
			var verr verror.E
			typ = reflect.ValueOf(&verr).Elem().Type()
		}
		outs[ix] = reflect.New(typ).Interface()
	}
	return outs
}

func checkResultPtrs(t *testing.T, name string, gotptrs, want []interface{}) {
	for ix, res := range gotptrs {
		got := reflect.ValueOf(res).Elem().Interface()
		want := want[ix]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s result %d got %v, want %v", name, ix, got, want)
		}
	}
}

func newID(name string) security.PrivateID {
	id, err := isecurity.NewPrivateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

var _ ipc.ClientOpt = vc.FixedLocalID(newID("irrelevant"))
var _ ipc.ServerOpt = vc.FixedLocalID(newID("irrelevant"))
