package ipc

import (
	"reflect"
	"testing"

	_ "veyron.io/veyron/veyron/lib/testutil"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
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

func newCaveat(v security.CaveatValidator) security.Caveat {
	cav, err := security.NewCaveat(v)
	if err != nil {
		panic(err)
	}
	return cav
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
