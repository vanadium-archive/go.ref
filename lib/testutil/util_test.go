package testutil_test

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"testing"

	"veyron/lib/testutil"
	isecurity "veyron/runtimes/google/security"

	"veyron2/rt"
	"veyron2/security"
)

func TestFormatLogline(t *testing.T) {
	depth, want := testutil.DepthToExternalCaller(), 2
	if depth != want {
		t.Errorf("got %v, want %v", depth, want)
	}
	{
		line, want := testutil.FormatLogLine(depth, "test"), "testing.go:.*"
		if ok, err := regexp.MatchString(want, line); !ok || err != nil {
			t.Errorf("got %v, want %v", line, want)
		}
	}
}

func TestSaveIdentityToFile(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}
	defer r.Shutdown()
	id, err := r.NewIdentity("test")
	if err != nil {
		t.Fatalf("r.NewIdentity failed: %v", err)
	}

	filePath := testutil.SaveIdentityToFile(id)
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

func TestNewBlessedIdentity(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}
	defer r.Shutdown()
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
		{blesser: testutil.NewBlessedIdentity(newID("google"), "alice"), blessingName: "tv", name: "PrivateID:google/alice/tv"},
	}
	for _, d := range testdata {
		if got, want := fmt.Sprintf("%s", testutil.NewBlessedIdentity(d.blesser, d.blessingName)), d.name; got != want {
			t.Errorf("NewBlessedIdentity(%q, %q): Got %q, want %q", d.blesser, d.blessingName, got, want)
		}
	}
}
