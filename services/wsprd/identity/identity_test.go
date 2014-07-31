package identity

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"veyron2"
	"veyron2/rt"
	"veyron2/security"
)

func createChain(r veyron2.Runtime, name string) security.PrivateID {
	id := r.Identity()

	for _, component := range strings.Split(name, "/") {
		newID, err := r.NewIdentity(component)
		if err != nil {
			panic(err)
		}
		if id == nil {
			id = newID
			continue
		}
		blessedID, err := id.Bless(newID.PublicID(), component, time.Hour, nil)
		if err != nil {
			panic(err)
		}
		id, err = newID.Derive(blessedID)
		if err != nil {
			panic(err)
		}
	}
	return id
}

func TestSavingAndFetchingIdentity(t *testing.T) {
	r := rt.Init()
	manager, err := NewIDManager(r, &InMemorySerializer{})
	if err != nil {
		t.Fatalf("creating identity manager failed with: %v", err)
	}
	manager.AddAccount("google/user1", createChain(r, "google/user1"))
	if err := manager.AddOrigin("sampleapp.com", "google/user1", nil); err != nil {
		t.Fatalf("failed to generate id: %v", err)
	}

	if _, err := manager.Identity("sampleapp.com"); err != nil {
		t.Errorf("failed to get  an identity for sampleapp.com: %v", err)
	}

	if _, err := manager.Identity("unknown.com"); err != OriginDoesNotExist {
		t.Error("should not have found an identity for unknown.com")
	}
}

func TestAccountsMatching(t *testing.T) {
	r := rt.Init()
	topLevelName := r.Identity().PublicID().Names()[0]
	manager, err := NewIDManager(r, &InMemorySerializer{})
	if err != nil {
		t.Fatalf("creating identity manager failed with: %v", err)
	}
	manager.AddAccount("google/user1", createChain(r, "google/user1"))
	manager.AddAccount("google/user2", createChain(r, "google/user2"))
	manager.AddAccount("facebook/user1", createChain(r, "facebook/user1"))

	result := manager.AccountsMatching(security.PrincipalPattern(topLevelName + "/google/*"))
	sort.StringSlice(result).Sort()
	expected := []string{"google/user1", "google/user2"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("unexpected result from AccountsMatching, expected :%v, got: %v", expected, result)
	}
}

func TestGenerateIDWithUnknownBlesser(t *testing.T) {
	r := rt.Init()
	manager, err := NewIDManager(r, &InMemorySerializer{})
	if err != nil {
		t.Fatalf("creating identity manager failed with: %v", err)
	}

	err = manager.AddOrigin("sampleapp.com", "google/user1", nil)

	if err == nil {
		t.Errorf("should have failed to generated an id blessed by google/user1")
	}
}

func TestSerializingAndDeserializing(t *testing.T) {
	r := rt.Init()
	var serializer InMemorySerializer

	manager, err := NewIDManager(r, &serializer)
	if err != nil {
		t.Fatalf("creating identity manager failed with: %v", err)
	}
	manager.AddAccount("google/user1", createChain(r, "google/user1"))
	if err = manager.AddOrigin("sampleapp.com", "google/user1", nil); err != nil {
		t.Fatalf("failed to generate id: %v", err)
	}

	newManager, err := NewIDManager(r, &serializer)

	if err != nil {
		t.Fatalf("failed to deserialize data: %v", err)
	}
	if _, err := newManager.Identity("sampleapp.com"); err != nil {
		t.Errorf("can't find the sampleapp.com identity: %v", err)
	}

	if err := newManager.AddOrigin("sampleapp2.com", "google/user1", nil); err != nil {
		t.Errorf("failed to create sampleapp2.com identity: %v", err)
	}
}
