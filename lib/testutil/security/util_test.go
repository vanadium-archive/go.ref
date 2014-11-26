package security

import (
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/security/access"

	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
)

func TestNewVeyronCredentials(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cleanup()

	parent := r.Principal()
	childdir := NewVeyronCredentials(parent, "child")
	if _, err = vsecurity.LoadPersistentPrincipal(childdir, nil); err != nil {
		t.Fatalf("Expected NewVeyronCredentials to have populated the directory %q: %v", childdir, err)
	}
	// TODO(ashankar,ataly): Figure out what APIs should we use for more detailed testing
	// of the child principal, for example:
	// - Parent should recognize the child's default blessing
	// - Child should recognize the parent's default blessing
	// - Child's blessing name should be that of the parent with "/child" appended.
}

func TestSaveACLToFile(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}
	defer r.Cleanup()
	acl := access.TaggedACLMap{
		"Admin": access.ACL{
			In:    []security.BlessingPattern{"comics/..."},
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
	if def == nil {
		t.Errorf("BlessingStore should have a default blessing")
	}
	if peers != def {
		t.Errorf("ForPeer(...) returned %v, want %v", peers, def)
	}
	// TODO(ashankar): Implement a security.Context and test the string
	// values as well.
}
