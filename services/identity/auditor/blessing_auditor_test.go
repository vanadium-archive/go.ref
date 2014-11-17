package auditor

import (
	"reflect"
	"testing"
	"time"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/audit"
	"veyron.io/veyron/veyron2/security"
)

func TestBlessingAuditor(t *testing.T) {
	db := &mockDatabase{}
	auditor, reader := &blessingAuditor{db}, &blessingLogReader{db}

	p, err := vsecurity.NewPrincipal()
	if err != nil {
		t.Fatalf("failed to create principal: %v", err)
	}
	expiryCaveat := newCaveat(security.ExpiryCaveat(time.Now().Add(time.Hour)))
	revocationCaveat := newThirdPartyCaveat(t, p)

	tests := []struct {
		Extension          string
		Email              string
		Caveats            []security.Caveat
		RevocationCaveatID string
		Blessings          security.Blessings
	}{
		{
			Extension:          "email/nocaveats",
			Email:              "email",
			RevocationCaveatID: "",
			Blessings:          newBlessing(t, p, "test/email/nocaveats"),
		},
		{
			Extension:          "email/caveat",
			Email:              "email",
			Caveats:            []security.Caveat{expiryCaveat},
			RevocationCaveatID: "",
			Blessings:          newBlessing(t, p, "test/email/caveat"),
		},
		{
			Extension:          "email/caveatAndRevocation",
			Email:              "email",
			Caveats:            []security.Caveat{expiryCaveat, newCaveat(security.NewCaveat(revocationCaveat))},
			RevocationCaveatID: revocationCaveat.ID(),
			Blessings:          newBlessing(t, p, "test/email/caveatAndRevocation"),
		},
	}

	for _, test := range tests {
		args := []interface{}{nil, nil, test.Extension}
		for _, cav := range test.Caveats {
			args = append(args, cav)
		}
		if err := auditor.Audit(audit.Entry{
			Method:    "Bless",
			Arguments: args,
			Results:   []interface{}{test.Blessings},
		}); err != nil {
			t.Errorf("Failed to audit Blessing %v: %v", test.Blessings, err)
		}
		ch := reader.Read("query")
		got := <-ch
		if got.Email != test.Email {
			t.Errorf("got %v, want %v", got.Email, test.Email)
		}
		if !reflect.DeepEqual(got.Caveats, test.Caveats) {
			t.Errorf("got %#v, want %#v", got.Caveats, test.Caveats)
		}
		if got.RevocationCaveatID != test.RevocationCaveatID {
			t.Errorf("got %v, want %v", got.RevocationCaveatID, test.RevocationCaveatID)
		}
		if !reflect.DeepEqual(got.Blessings, test.Blessings) {
			t.Errorf("got %v, want %v", got.Blessings, test.Blessings)
		}
		var extra bool
		for _ = range ch {
			// Drain the channel to prevent the producer goroutines from being leaked.
			extra = true
		}
		if extra {
			t.Errorf("Got more entries that expected for test %+v", test)
		}
	}
}

type mockDatabase struct {
	NextEntry databaseEntry
}

func (db *mockDatabase) Insert(entry databaseEntry) error {
	db.NextEntry = entry
	return nil
}
func (db *mockDatabase) Query(email string) <-chan databaseEntry {
	c := make(chan databaseEntry)
	go func() {
		c <- db.NextEntry
		close(c)
	}()
	return c
}

func newThirdPartyCaveat(t *testing.T, p security.Principal) security.ThirdPartyCaveat {
	tp, err := security.NewPublicKeyCaveat(p.PublicKey(), "location", security.ThirdPartyRequirements{}, newCaveat(security.MethodCaveat("method")))
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

func newBlessing(t *testing.T, p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newCaveat(caveat security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return caveat
}
