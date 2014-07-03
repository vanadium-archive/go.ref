package security

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	isecurity "veyron/runtimes/google/security"

	"veyron2/rt"
	"veyron2/security"
)

func TestNewBlessedIdentity(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}
	defer r.Cleanup()
	newID := func(name string) security.PrivateID {
		id, err := r.NewIdentity(name)
		if err != nil {
			t.Fatalf("r.NewIdentity failed: %v", err)
		}
		isecurity.TrustIdentityProviders(id)
		return id
	}
	testdata := []struct {
		blesser            security.PrivateID
		blessingName, name string
	}{
		{blesser: newID("google"), blessingName: "alice", name: "PrivateID:google/alice"},
		{blesser: newID("google"), blessingName: "bob", name: "PrivateID:google/bob"},
		{blesser: newID("veyron"), blessingName: "alice", name: "PrivateID:veyron/alice"},
		{blesser: newID("veyron"), blessingName: "bob", name: "PrivateID:veyron/bob"},
		{blesser: NewBlessedIdentity(newID("google"), "alice"), blessingName: "tv", name: "PrivateID:google/alice/tv"},
	}
	for _, d := range testdata {
		if got, want := fmt.Sprintf("%s", NewBlessedIdentity(d.blesser, d.blessingName)), d.name; got != want {
			t.Errorf("NewBlessedIdentity(%q, %q): Got %q, want %q", d.blesser, d.blessingName, got, want)
		}
	}
}

func TestSaveACLToFile(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}
	defer r.Cleanup()
	acl := security.ACL{
		"veyron/alice": security.LabelSet(security.ReadLabel | security.WriteLabel),
		"veyron/bob":   security.LabelSet(security.ReadLabel),
	}

	filePath := SaveACLToFile(acl)
	defer os.Remove(filePath)

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open(%v) failed: %v", filePath, err)
	}
	defer f.Close()
	loadedACL, err := security.LoadACL(f)
	if err != nil {
		t.Fatalf("LoadACL failed: %v", err)
	}
	if !reflect.DeepEqual(loadedACL, acl) {
		t.Fatalf("Got ACL %v, but want %v", loadedACL, acl)
	}
}

func TestSaveIdentityToFile(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}
	defer r.Cleanup()
	id, err := r.NewIdentity("test")
	if err != nil {
		t.Fatalf("r.NewIdentity failed: %v", err)
	}

	filePath := SaveIdentityToFile(id)
	defer os.Remove(filePath)

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open(%v) failed: %v", filePath, err)
	}
	defer f.Close()
	loadedID, err := security.LoadIdentity(f)
	if err != nil {
		t.Fatalf("LoadIdentity failed: %v", err)
	}
	if !reflect.DeepEqual(loadedID, id) {
		t.Fatalf("Got Identity %v, but want %v", loadedID, id)
	}
}
