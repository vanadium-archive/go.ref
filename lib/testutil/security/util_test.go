package security

import (
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"

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
	_, existed, err := vsecurity.NewPersistentPrincipal(childdir)
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Fatalf("Expected NewVeyronCredentials to have populated the directory %q", childdir)
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
	acl := security.ACL{}
	acl.In = map[security.BlessingPattern]security.LabelSet{
		"veyron/...":   security.LabelSet(security.ReadLabel),
		"veyron/alice": security.LabelSet(security.ReadLabel | security.WriteLabel),
		"veyron/bob":   security.LabelSet(security.AdminLabel),
	}
	acl.NotIn = map[string]security.LabelSet{
		"veyron/che": security.LabelSet(security.ReadLabel),
	}

	filePath := SaveACLToFile(acl)
	defer os.Remove(filePath)

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open(%v) failed: %v", filePath, err)
	}
	defer f.Close()
	loadedACL, err := vsecurity.LoadACL(f)
	if err != nil {
		t.Fatalf("LoadACL failed: %v", err)
	}
	if !reflect.DeepEqual(loadedACL, acl) {
		t.Fatalf("Got ACL %v, but want %v", loadedACL, acl)
	}
}
