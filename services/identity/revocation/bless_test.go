package revocation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"veyron.io/veyron/veyron/security/audit"
	"veyron.io/veyron/veyron/services/security/discharger"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
)

type auditor struct {
	LastEntry audit.Entry
}

func (a *auditor) Audit(entry audit.Entry) error {
	a.LastEntry = entry
	return nil
}

func newAuditedPrivateID(a *auditor) (security.PrivateID, error) {
	r, err := rt.New()
	if err != nil {
		return nil, err
	}
	defer r.Cleanup()
	id := r.Identity()
	if err != nil {
		return nil, err
	}
	return audit.NewPrivateID(id, a), err
}

func TestReadBlessAudit(t *testing.T) {
	var a auditor
	var revocationDir = filepath.Join(os.TempDir(), "util_bless_test_dir")
	os.MkdirAll(revocationDir, 0700)
	defer os.RemoveAll(revocationDir)

	self, err := newAuditedPrivateID(&a)
	if err != nil {
		t.Fatalf("failed to create new audited private id: %v", err)
	}

	// Test caveat
	correct_blessee := self.PublicID()

	_, cav, err := discharger.NewRevocationCaveat(self.PublicID(), "")
	if err != nil {
		t.Fatalf("discharger.NewRevocationCaveat failed: %v", err)
	}

	correct_blessed, err := Bless(self, self.PublicID(), "test", time.Second, nil, cav)
	if err != nil {
		t.Fatalf("Bless: failed with caveats: %v", err)
	}

	var blessEntry BlessingAuditEntry
	blessEntry, err = ReadBlessAuditEntry(a.LastEntry)
	if err != nil {
		t.Fatal("ReadBlessAuditEntryFailed %v:", err)
	}
	if !reflect.DeepEqual(blessEntry.Blessee, correct_blessee) {
		t.Errorf("blessee incorrect: expected %v got %v", correct_blessee, blessEntry.Blessee)
	}
	if !reflect.DeepEqual(blessEntry.Blessed, correct_blessed) {
		t.Errorf("blessed incorrect: expected %v got %v", correct_blessed, blessEntry.Blessed)
	}
	if blessEntry.RevocationCaveat.ID() != cav.ID() {
		t.Errorf("caveat ID incorrect: expected %s got %s", cav.ID(), blessEntry.RevocationCaveat.ID())
	}

	// Test no caveat
	correct_blessed, err = Bless(self, self.PublicID(), "test", time.Second, nil, nil)
	if err != nil {
		t.Fatalf("Bless: failed with no caveats: %v", err)
	}

	blessEntry, err = ReadBlessAuditEntry(a.LastEntry)
	if err != nil {
		t.Fatal("ReadBlessAuditEntryFailed %v:", err)
	}
	if !reflect.DeepEqual(blessEntry.Blessee, correct_blessee) {
		t.Errorf("blessee incorrect: expected %v got %v", correct_blessee, blessEntry.Blessee)
	}
	if !reflect.DeepEqual(blessEntry.Blessed, correct_blessed) {
		t.Errorf("blessed incorrect: expected %v got %v", correct_blessed, blessEntry.Blessed)
	}
	if blessEntry.RevocationCaveat != nil {
		t.Errorf("caveat ID incorrect: expected %s got %s", cav.ID(), blessEntry.RevocationCaveat.ID())
	}
}
