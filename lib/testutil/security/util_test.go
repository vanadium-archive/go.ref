package security

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"v.io/v23/security"
	"v.io/v23/services/security/access"

	_ "v.io/core/veyron/profiles"
	vsecurity "v.io/core/veyron/security"
)

func unsortedEquals(a, b []string) bool {
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func namesForBlessings(p security.Principal, b security.Blessings) (blessings []string) {
	for name, _ := range p.BlessingsInfo(b) {
		blessings = append(blessings, name)
	}
	return
}

func testCredentials(cred string, wantPrincipal security.Principal, wantBlessings []string) error {
	pFromCred, err := vsecurity.LoadPersistentPrincipal(cred, nil)
	if err != nil {
		return fmt.Errorf("LoadPersistentPrincipal(%q, nil) failed: %v", cred, err)
	}
	if !reflect.DeepEqual(pFromCred, wantPrincipal) {
		fmt.Errorf("got principal from directory: %v, want: %v", pFromCred, wantPrincipal)
	}

	bs := pFromCred.BlessingStore()
	if got := namesForBlessings(pFromCred, bs.ForPeer("foo")); !unsortedEquals(got, wantBlessings) {
		return fmt.Errorf("got peer blessings: %v, want: %v", got, wantBlessings)
	}
	if got := namesForBlessings(pFromCred, bs.Default()); !unsortedEquals(got, wantBlessings) {
		return fmt.Errorf("got default blessings: %v, want: %v", got, wantBlessings)
	}
	return nil
}

func TestCredentials(t *testing.T) {
	dir, p := NewCredentials("ali", "alice")
	if err := testCredentials(dir, p, []string{"ali", "alice"}); err != nil {
		t.Fatal(err)
	}

	forkdir, forkp := ForkCredentials(p, "friend", "enemy")
	if err := testCredentials(forkdir, forkp, []string{"ali/friend", "alice/friend", "ali/enemy", "alice/enemy"}); err != nil {
		t.Fatal(err)
	}

	forkforkdir, forkforkp := ForkCredentials(forkp, "spouse")
	if err := testCredentials(forkforkdir, forkforkp, []string{"ali/friend/spouse", "alice/friend/spouse", "ali/enemy/spouse", "alice/enemy/spouse"}); err != nil {
		t.Fatal(err)
	}
}

func TestSaveACLToFile(t *testing.T) {
	acl := access.TaggedACLMap{
		"Admin": access.ACL{
			In:    []security.BlessingPattern{"comics"},
			NotIn: []string{"comics/villain"},
		},
	}

	filePath := SaveACLToFile(acl)
	defer os.Remove(filePath)

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open(%v) failed: %v", filePath, err)
	}
	defer f.Close()
	loadedACL, err := access.ReadTaggedACLMap(f)
	if err != nil {
		t.Fatalf("LoadACL failed: %v", err)
	}
	if !reflect.DeepEqual(loadedACL, acl) {
		t.Fatalf("Got %#v, want %#v", loadedACL, acl)
	}
}

func TestIDProvider(t *testing.T) {
	idp := NewIDProvider("foo")
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(p, "bar"); err != nil {
		t.Fatal(err)
	}
	if err := p.Roots().Recognized(idp.PublicKey(), "foo"); err != nil {
		t.Error(err)
	}
	if err := p.Roots().Recognized(idp.PublicKey(), "foo/bar"); err != nil {
		t.Error(err)
	}
	def := p.BlessingStore().Default()
	peers := p.BlessingStore().ForPeer("anyone_else")
	if def.IsZero() {
		t.Errorf("BlessingStore should have a default blessing")
	}
	if !reflect.DeepEqual(peers, def) {
		t.Errorf("ForPeer(...) returned %v, want %v", peers, def)
	}
	// TODO(ashankar): Implement a security.Context and test the string
	// values as well.
}
