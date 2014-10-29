package blesser

import (
	"bytes"
	"crypto/rand"
	"reflect"
	"testing"
	"time"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/services/identity"
	"veyron.io/veyron/veyron/services/identity/util"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

func TestMacaroonBlesser(t *testing.T) {
	var (
		key            = make([]byte, 16)
		provider, user = newPrincipal(t), newPrincipal(t)
		cOnlyMethodFoo = newCaveat(security.MethodCaveat("Foo"))
		context        = &serverCall{
			p:      provider,
			local:  blessSelf(t, provider, "provider"),
			remote: blessSelf(t, user, "self-signed-user"),
		}
	)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	blesser := NewMacaroonBlesserServer(key).(*identity.ServerStubMacaroonBlesser)

	m := BlessingMacaroon{Creation: time.Now().Add(-1 * time.Hour), Name: "foo"}
	if got, err := blesser.Bless(context, newMacaroon(t, key, m)); err == nil || err.Error() != "macaroon has expired" {
		t.Errorf("Got (%v, %v)", got, err)
	}
	m = BlessingMacaroon{Creation: time.Now(), Name: "user", Caveats: []security.Caveat{cOnlyMethodFoo}}
	if result, err := blesser.Bless(context, newMacaroon(t, key, m)); err != nil {
		t.Errorf("Got (%v, %v)", result, err)
	} else {
		b, err := security.NewBlessings(result)
		if err != nil {
			t.Fatalf("Unable to decode response into a security.Blessings object: %v", err)
		}
		if !reflect.DeepEqual(b.PublicKey(), user.PublicKey()) {
			t.Errorf("Received blessing for public key %v. Client:%v, Blesser:%v", b.PublicKey(), user.PublicKey(), provider.PublicKey())
		}
		// Context at a server to which the user will present her blessings.
		server := newPrincipal(t)
		serverCtxNoMethod := &serverCall{
			p:      server,
			remote: b,
		}
		serverCtxFoo := &serverCall{
			p:      server,
			remote: b,
			method: "Foo",
		}
		// When the server does not recognize the provider, it should not see any strings for the client's blessings.
		if got := b.ForContext(serverCtxNoMethod); len(got) > 0 {
			t.Errorf("Got blessing that returned %v for an empty security.Context (%v)", got, b)
		}
		if got := b.ForContext(serverCtxFoo); len(got) > 0 {
			t.Errorf("Got blessing that returned %v for an empty security.Context (%v)", got, b)
		}
		// But once it recognizes the provider, serverCtxFoo should see the "provider/user" name.
		server.AddToRoots(b)
		if got := b.ForContext(serverCtxNoMethod); len(got) > 0 {
			t.Errorf("Got blessing that returned %v for an empty security.Context (%v)", got, b)
		}
		if got, want := b.ForContext(serverCtxFoo), []string{"provider/user"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Got %v, want %v", got, want)
		}
	}
}

type serverCall struct {
	ipc.ServerCall
	method        string
	p             security.Principal
	local, remote security.Blessings
}

func (c *serverCall) Method() string                      { return c.method }
func (c *serverCall) LocalPrincipal() security.Principal  { return c.p }
func (c *serverCall) LocalBlessings() security.Blessings  { return c.local }
func (c *serverCall) RemoteBlessings() security.Blessings { return c.remote }

func newPrincipal(t *testing.T) security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	return p
}

func blessSelf(t *testing.T, p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newCaveat(c security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return c
}

func newMacaroon(t *testing.T, key []byte, m BlessingMacaroon) string {
	buf := new(bytes.Buffer)
	if err := vom.NewEncoder(buf).Encode(m); err != nil {
		t.Fatal(err)
	}
	return string(util.NewMacaroon(key, buf.Bytes()))
}
