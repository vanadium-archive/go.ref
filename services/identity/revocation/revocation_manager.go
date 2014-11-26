// Package revocation provides tools to create and manage revocation caveats.
package revocation

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
)

// RevocationManager persists information for revocation caveats to provided discharges and allow for future revocations.
type RevocationManager struct{}

// NewRevocationManager returns a RevocationManager that persists information about
// revocationCaveats in a SQL database and allows for revocation and caveat creation.
// This function can only be called once because of the use of global variables.
func NewRevocationManager(sqlDB *sql.DB) (*RevocationManager, error) {
	revocationLock.Lock()
	defer revocationLock.Unlock()
	if revocationDB != nil {
		return nil, fmt.Errorf("NewRevocationManager can only be called once")
	}
	var err error
	revocationDB, err = newSQLDatabase(sqlDB, "RevocationCaveatInfo")
	if err != nil {
		return nil, err
	}
	return &RevocationManager{}, nil
}

var revocationDB database
var revocationLock sync.RWMutex

// NewCaveat returns a security.Caveat constructed with a ThirdPartyCaveat for which discharges will be
// issued iff Revoke has not been called for the returned caveat.
func (r *RevocationManager) NewCaveat(discharger security.PublicKey, dischargerLocation string) (security.Caveat, error) {
	var empty security.Caveat
	var revocation [16]byte
	if _, err := rand.Read(revocation[:]); err != nil {
		return empty, err
	}
	restriction, err := security.NewCaveat(revocationCaveat(revocation))
	if err != nil {
		return empty, err
	}
	cav, err := security.NewPublicKeyCaveat(discharger, dischargerLocation, security.ThirdPartyRequirements{}, restriction)
	if err != nil {
		return empty, err
	}
	if err = revocationDB.InsertCaveat(cav.ID(), revocation[:]); err != nil {
		return empty, err
	}
	return security.NewCaveat(cav)
}

// Revoke disables discharges from being issued for the provided third-party caveat.
func (r *RevocationManager) Revoke(caveatID string) error {
	return revocationDB.Revoke(caveatID)
}

// GetRevocationTimestamp returns the timestamp at which a caveat was revoked.
// If the caveat wasn't revoked returns nil
func (r *RevocationManager) GetRevocationTime(caveatID string) *time.Time {
	timestamp, err := revocationDB.RevocationTime(caveatID)
	if err != nil {
		return nil
	}
	return timestamp
}

type revocationCaveat [16]byte

func (cav revocationCaveat) Validate(security.Context) error {
	revocationLock.RLock()
	if revocationDB == nil {
		revocationLock.RUnlock()
		return fmt.Errorf("missing call to NewRevocationManager")
	}
	revocationLock.RUnlock()
	revoked, err := revocationDB.IsRevoked(cav[:])
	if revoked {
		return fmt.Errorf("revoked")
	}
	return err
}

func init() {
	vdlutil.Register(revocationCaveat{})
}
