package security

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"veyron2/security"
	"veyron2/vom"
)

func TestLoadSaveIdentity(t *testing.T) {
	id := security.FakePrivateID("test")

	var buf bytes.Buffer
	if err := SaveIdentity(&buf, id); err != nil {
		t.Fatalf("Failed to save PrivateID %q: %v", id, err)
	}

	loadedID, err := LoadIdentity(&buf)
	if err != nil {
		t.Fatalf("Failed to load PrivateID: %v", err)
	}
	if !reflect.DeepEqual(loadedID, id) {
		t.Fatalf("Got Identity %v, but want %v", loadedID, id)
	}
}

func TestLoadSaveACL(t *testing.T) {
	acl := security.ACL{}
	acl.In = map[security.BlessingPattern]security.LabelSet{
		"veyron/...":   security.LabelSet(security.ReadLabel),
		"veyron/alice": security.LabelSet(security.ReadLabel | security.WriteLabel),
		"veyron/bob":   security.LabelSet(security.AdminLabel),
	}
	acl.NotIn = map[string]security.LabelSet{
		"veyron/che": security.LabelSet(security.ReadLabel),
	}

	var buf bytes.Buffer
	if err := SaveACL(&buf, acl); err != nil {
		t.Fatalf("Failed to save ACL %q: %v", acl, err)
	}

	loadedACL, err := LoadACL(&buf)
	if err != nil {
		t.Fatalf("Failed to load ACL: %v", err)
	}
	if !reflect.DeepEqual(loadedACL, acl) {
		t.Fatalf("Got ACL %v, but want %v", loadedACL, acl)
	}
}

// fpCaveat implements security.CaveatValidator.
type fpCaveat struct{}

func (fpCaveat) Validate(security.Context) error { return nil }

// tpCaveat implements security.ThirdPartyCaveat.
type tpCaveat struct{}

func (tpCaveat) Validate(security.Context) (err error)             { return }
func (tpCaveat) ID() (id string)                                   { return }
func (tpCaveat) Location() (loc string)                            { return }
func (tpCaveat) Requirements() (r security.ThirdPartyRequirements) { return }

func TestCaveatUtil(t *testing.T) {
	type C []security.Caveat
	type V []security.CaveatValidator
	type TP []security.ThirdPartyCaveat

	newCaveat := func(v security.CaveatValidator) security.Caveat {
		c, err := security.NewCaveat(v)
		if err != nil {
			t.Fatalf("failed to create Caveat from validator %T: %v", v, c)
		}
		return c
	}

	var (
		fp      fpCaveat
		tp      tpCaveat
		invalid = security.Caveat{ValidatorVOM: []byte("invalid")}
	)
	testdata := []struct {
		caveats    []security.Caveat
		validators []security.CaveatValidator
		tpCaveats  []security.ThirdPartyCaveat
	}{
		{nil, nil, nil},
		{C{newCaveat(fp)}, V{fp}, nil},
		{C{newCaveat(tp)}, V{tp}, TP{tp}},
		{C{newCaveat(fp), newCaveat(tp)}, V{fp, tp}, TP{tp}},
	}
	for i, d := range testdata {
		// Test CaveatValidators.
		got, err := CaveatValidators(d.caveats...)
		if err != nil {
			t.Errorf("CaveatValidators(%v) failed: %s", d.caveats, err)
			continue
		}
		if !reflect.DeepEqual(got, d.validators) {
			fmt.Println("TEST ", i)
			t.Errorf("CaveatValidators(%v): got: %#v, want: %#v", d.caveats, got, d.validators)
			continue
		}
		if _, err := CaveatValidators(append(d.caveats, invalid)...); err == nil {
			t.Errorf("CaveatValidators(%v) succeeded unexpectedly", d.caveats)
			continue
		}
		// Test ThirdPartyCaveats.
		if got := ThirdPartyCaveats(d.caveats...); !reflect.DeepEqual(got, d.tpCaveats) {
			t.Errorf("ThirdPartyCaveats(%v): got: %#v, want: %#v", d.caveats, got, d.tpCaveats)
			continue
		}
		if got := ThirdPartyCaveats(append(d.caveats, invalid)...); !reflect.DeepEqual(got, d.tpCaveats) {
			t.Errorf("ThirdPartyCaveats(%v): got: %#v, want: %#v", d.caveats, got, d.tpCaveats)
			continue
		}
	}
}

func init() {
	vom.Register(&fpCaveat{})
	vom.Register(&tpCaveat{})
}
