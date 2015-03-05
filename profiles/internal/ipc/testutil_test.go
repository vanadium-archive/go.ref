package ipc

import (
	"reflect"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/verror"
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
			if !verror.Is(g, w.ID) {
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

// We need a special way to create contexts for tests.  We
// can't create a real runtime in the runtime implementation
// so we use a fake one that panics if used.  The runtime
// implementation should not ever use the Runtime from a context.
func testContext() *context.T {
	ctx, _ := context.WithTimeout(testContextWithoutDeadline(), 20*time.Second)
	return ctx
}

type mockSecurityContext struct {
	p    security.Principal
	l, r security.Blessings
	c    *context.T
}

func (c *mockSecurityContext) Timestamp() (t time.Time)                        { return }
func (c *mockSecurityContext) Method() string                                  { return "" }
func (c *mockSecurityContext) MethodTags() []*vdl.Value                        { return nil }
func (c *mockSecurityContext) Suffix() string                                  { return "" }
func (c *mockSecurityContext) LocalDischarges() map[string]security.Discharge  { return nil }
func (c *mockSecurityContext) RemoteDischarges() map[string]security.Discharge { return nil }
func (c *mockSecurityContext) LocalEndpoint() naming.Endpoint                  { return nil }
func (c *mockSecurityContext) RemoteEndpoint() naming.Endpoint                 { return nil }
func (c *mockSecurityContext) LocalPrincipal() security.Principal              { return c.p }
func (c *mockSecurityContext) LocalBlessings() security.Blessings              { return c.l }
func (c *mockSecurityContext) RemoteBlessings() security.Blessings             { return c.r }
func (c *mockSecurityContext) Context() *context.T                             { return c.c }
