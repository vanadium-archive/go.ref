package security

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"v.io/v23/security"
	"v.io/v23/services/security/access"

	_ "v.io/x/ref/profiles"
	vsecurity "v.io/x/ref/security"
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

func testBlessings(principal security.Principal, wantBlessings ...string) error {
	bs := principal.BlessingStore()
	if got := namesForBlessings(principal, bs.ForPeer("foo")); !unsortedEquals(got, wantBlessings) {
		return fmt.Errorf("got peer blessings: %v, want: %v", got, wantBlessings)
	}
	if got := namesForBlessings(principal, bs.Default()); !unsortedEquals(got, wantBlessings) {
		return fmt.Errorf("got default blessings: %v, want: %v", got, wantBlessings)
	}
	return nil
}

func TestForkCredentials(t *testing.T) {
	dir, origp := NewCredentials("ali", "alice")
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatalf("LoadPersistentPrincipal(%q, nil) failed: %v", dir, err)
	}
	if !reflect.DeepEqual(p, origp) {
		t.Errorf("LoadPersistentPrincipal(%q) returned %v, want %v", dir, p, origp)
	}
	if err := testBlessings(p, "ali", "alice"); err != nil {
		t.Fatal(err)
	}
	forkp := ForkCredentials(p, "friend", "enemy")
	if err := testBlessings(forkp, "ali/friend", "alice/friend", "ali/enemy", "alice/enemy"); err != nil {
		t.Fatal(err)
	}
	forkforkp := ForkCredentials(forkp, "spouse")
	if err := testBlessings(forkforkp, "ali/friend/spouse", "alice/friend/spouse", "ali/enemy/spouse", "alice/enemy/spouse"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveAccessListToFile(t *testing.T) {
	acl := access.Permissions{
		"Admin": access.AccessList{
			In:    []security.BlessingPattern{"comics"},
			NotIn: []string{"comics/villain"},
		},
	}

	filePath := SaveAccessListToFile(acl)
	defer os.Remove(filePath)

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open(%v) failed: %v", filePath, err)
	}
	defer f.Close()
	loadedAccessList, err := access.ReadPermissions(f)
	if err != nil {
		t.Fatalf("LoadAccessList failed: %v", err)
	}
	if !reflect.DeepEqual(loadedAccessList, acl) {
		t.Fatalf("Got %#v, want %#v", loadedAccessList, acl)
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
	// TODO(ashankar): Implement a security.Call and test the string
	// values as well.
}
