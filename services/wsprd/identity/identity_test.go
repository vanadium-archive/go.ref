package identity

import (
	"net/url"
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
	origin := "http://sampleapp.com:80"
	account := "google/user1"
	manager.AddAccount(account, createChain(r, account))
	if err := manager.AddOrigin(origin, account, nil); err != nil {
		t.Fatalf("failed to generate id: %v", err)
	}

	id, err := manager.Identity(origin)
	if err != nil {
		t.Errorf("failed to get an identity for %v: %v", origin, err)
	}
	want := []string{createChain(r, account).PublicID().Names()[0] + "/" + url.QueryEscape(origin)}
	if got := id.PublicID().Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected identity name. got: %v, wanted: %v", got, want)
	}

	unknownOrigin := "http://unknown.com:80"
	if _, err := manager.Identity(unknownOrigin); err != OriginDoesNotExist {
		t.Error("should not have found an identity for %v", unknownOrigin)
	}
}

func TestAccountsMatching(t *testing.T) {
	r := rt.Init()
	topLevelName := r.Identity().PublicID().Names()[0]
	manager, err := NewIDManager(r, &InMemorySerializer{})
	if err != nil {
		t.Fatalf("creating identity manager failed with: %v", err)
	}
	googleAccount1 := "google/user1"
	googleAccount2 := "google/user2"
	facebookAccount := "facebook/user1"
	manager.AddAccount(googleAccount1, createChain(r, googleAccount1))
	manager.AddAccount(googleAccount2, createChain(r, googleAccount2))
	manager.AddAccount(facebookAccount, createChain(r, facebookAccount))

	result := manager.AccountsMatching(security.BlessingPattern(topLevelName + "/google/..."))
	sort.StringSlice(result).Sort()
	expected := []string{googleAccount1, googleAccount2}
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

	err = manager.AddOrigin("http://sampleapp.com:80", "google/user1", nil)

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
	origin1 := "https://sampleapp-1.com:443"
	account := "google/user1"
	if err = manager.AddOrigin(origin1, account, nil); err != nil {
		t.Fatalf("failed to generate id: %v", err)
	}

	newManager, err := NewIDManager(r, &serializer)

	if err != nil {
		t.Fatalf("failed to deserialize data: %v", err)
	}
	id, err := newManager.Identity(origin1)
	if err != nil {
		t.Errorf("can't find the %v identity: %v", origin1, err)
	}
	want := []string{createChain(r, account).PublicID().Names()[0] + "/" + url.QueryEscape(origin1)}
	if got := id.PublicID().Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected identity name. got: %v, wanted: %v", got, want)
	}

	origin2 := "https://sampleapp-2.com:443"
	if err := newManager.AddOrigin(origin2, account, nil); err != nil {
		t.Errorf("can't find the %v identity: %v", origin2, err)
	}
}
