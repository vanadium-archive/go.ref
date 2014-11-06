package acl

import (
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/acl/test"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
)

// TestTaggedACLAuthorizer is both a test and a demonstration of the use of the
// TaggedACLAuthorizer and interaction with interface specification in VDL.
func TestTaggedACLAuthorizer(t *testing.T) {
	type P []security.BlessingPattern
	type S []string
	// TaggedACLMap to test against.
	acl := TaggedACLMap{
		"R": {
			In: P{"..."},
		},
		"W": {
			In:    P{"ali/family/...", "bob/...", "che"},
			NotIn: S{"bob/acquaintances"},
		},
		"X": {
			In: P{"ali/family/boss", "superman"},
		},
	}
	type testcase struct {
		Method string
		Client security.Blessings
	}
	var (
		authorizer, _ = TaggedACLAuthorizer(acl, reflect.TypeOf(test.Read))
		// Two principals: The "server" and the "client"
		pserver, _ = vsecurity.NewPrincipal()
		pclient, _ = vsecurity.NewPrincipal()
		server, _  = pserver.BlessSelf("server")

		// B generates the provided blessings for the client and ensures
		// that the server will recognize them.
		B = func(names ...string) security.Blessings {
			var ret security.Blessings
			for _, name := range names {
				b, err := pclient.BlessSelf(name)
				if err != nil {
					t.Fatalf("%q: %v", name, err)
				}
				if err := pserver.AddToRoots(b); err != nil {
					t.Fatalf("%q: %v", name, err)
				}
				if ret, err = security.UnionOfBlessings(ret, b); err != nil {
					t.Fatal(err)
				}
			}
			return ret
		}

		run = func(test testcase) error {
			ctx := &context{
				localP: pserver,
				local:  server,
				remote: test.Client,
				method: test.Method,
			}
			return authorizer.Authorize(ctx)
		}
	)

	// Test cases where access should be granted to methods with tags on
	// them.
	for _, test := range []testcase{
		{"Get", nil},
		{"Get", B("ali")},
		{"Get", B("bob/friend", "che/enemy")},

		{"Put", B("ali")},
		{"Put", B("ali/family/mom")},
		{"Put", B("bob/friends")},
		{"Put", B("bob/acquantainces/carol", "che")}, // Access granted because of "che"

		{"Resolve", B("ali")},
		{"Resolve", B("ali/family/boss")},

		{"AllTags", B("ali/family/boss")},
	} {
		if err := run(test); err != nil {
			t.Errorf("Access denied to method %q to %v: %v", test.Method, test.Client, err)
		}
	}
	// Test cases where access should be denied.
	for _, test := range []testcase{
		// Nobody is denied access to "Get"
		{"Put", B("bob/acquaintances/dave", "che/friend", "dave")},
		{"Resolve", B("ali/family/friend")},
		// Since there are no tags on the NoTags method, it has an
		// empty ACL.  No client will have access.
		{"NoTags", B("ali", "ali/family/boss", "bob")},
		// On a method with multiple tags on it, all must be satisfied.
		{"AllTags", B("superman")},          // Only in the X ACL, not in R or W
		{"AllTags", B("superman", "clark")}, // In X and in R, but not W
	} {
		if err := run(test); err == nil {
			t.Errorf("Access to %q granted to %v", test.Method, test.Client)
		}
	}
}

func TestTaggedACLAuthorizerSelfRPCs(t *testing.T) {
	var (
		// Client and server are the same principal, though have
		// different blessings.
		p, _      = vsecurity.NewPrincipal()
		client, _ = p.BlessSelf("client")
		server, _ = p.BlessSelf("server")
		// Authorizer with a TaggedACLMap that grants read access to
		// anyone, write/execute access to noone.
		typ           test.MyTag
		authorizer, _ = TaggedACLAuthorizer(TaggedACLMap{"R": {In: []security.BlessingPattern{"nobody"}}}, reflect.TypeOf(typ))
	)
	for _, test := range []string{"Put", "Get", "Resolve", "NoTags", "AllTags"} {
		if err := authorizer.Authorize(&context{localP: p, local: server, remote: client, method: test}); err != nil {
			t.Errorf("Got error %v for method %q", err, test)
		}
	}
}

func TestTaggedACLAuthorizerWithNilACL(t *testing.T) {
	var (
		authorizer, _ = TaggedACLAuthorizer(nil, reflect.TypeOf(test.Read))
		pserver, _    = vsecurity.NewPrincipal()
		pclient, _    = vsecurity.NewPrincipal()
		server, _     = pserver.BlessSelf("server")
		client, _     = pclient.BlessSelf("client")
	)
	for _, test := range []string{"Put", "Get", "Resolve", "NoTags", "AllTags"} {
		if err := authorizer.Authorize(&context{localP: pserver, local: server, remote: client, method: test}); err == nil {
			t.Errorf("nil TaggedACLMap authorized method %q", test)
		}
	}
}

func TestTaggedACLAuthorizerFromFile(t *testing.T) {
	file, err := ioutil.TempFile("", "TestTaggedACLAuthorizerFromFile")
	if err != nil {
		t.Fatal(err)
	}
	filename := file.Name()
	file.Close()

	var (
		authorizer, _  = TaggedACLAuthorizerFromFile(filename, reflect.TypeOf(test.Read))
		pserver, _     = vsecurity.NewPrincipal()
		pclient, _     = vsecurity.NewPrincipal()
		server, _      = pserver.BlessSelf("alice")
		alicefriend, _ = pserver.Bless(pclient.PublicKey(), server, "friend/bob", security.UnconstrainedUse())
		ctx            = &context{localP: pserver, local: server, remote: alicefriend, method: "Get"}
	)
	// Make pserver recognize itself as an authority on "alice/..." blessings.
	if err := pserver.AddToRoots(server); err != nil {
		t.Fatal(err)
	}
	// "alice/friend/bob" should not have access to test.Read methods like Get.
	if err := authorizer.Authorize(ctx); err == nil {
		t.Fatalf("Expected authorization error as %v is not on the ACL for Read operations", ctx.remote)
	}
	// Rewrite the file giving access
	if err := ioutil.WriteFile(filename, []byte(`{"R": { "In":["alice/friend/..."] }}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Now should have access
	if err := authorizer.Authorize(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTagTypeMustBeString(t *testing.T) {
	type I int
	if auth, err := TaggedACLAuthorizer(TaggedACLMap{}, reflect.TypeOf(I(0))); err == nil || auth != nil {
		t.Errorf("Got (%v, %v), wanted error since tag type is not a string", auth, err)
	}
	if auth, err := TaggedACLAuthorizerFromFile("does_not_matter", reflect.TypeOf(I(0))); err == nil || auth != nil {
		t.Errorf("Got (%v, %v), wanted error since tag type is not a string", auth, err)
	}
}

// context implements security.Context.
type context struct {
	localP        security.Principal
	local, remote security.Blessings
	method        string
}

func (*context) Timestamp() (t time.Time)                  { return t }
func (c *context) Method() string                          { return c.method }
func (*context) Name() string                              { return "" }
func (*context) Suffix() string                            { return "" }
func (*context) Label() (l security.Label)                 { return l }
func (*context) Discharges() map[string]security.Discharge { return nil }
func (c *context) LocalPrincipal() security.Principal      { return c.localP }
func (c *context) LocalBlessings() security.Blessings      { return c.local }
func (c *context) RemoteBlessings() security.Blessings     { return c.remote }
func (*context) LocalEndpoint() naming.Endpoint            { return nil }
func (*context) RemoteEndpoint() naming.Endpoint           { return nil }
func (c *context) MethodTags() []interface{} {
	server := &test.ServerStubMyObject{}
	tags, _ := server.GetMethodTags(nil, c.method)
	return tags
}
