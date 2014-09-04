// Package revocation provides tools to create and manage revocation caveats.
package revocation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"veyron/security/caveat"
	"veyron/services/identity/util"
	"veyron2/security"
	"veyron2/vom"
)

// RevocationManager persists information for revocation caveats to provided discharges and allow for future revocations.
type RevocationManager struct {
	caveatMap *util.DirectoryStore // Map of blessed identity's caveats.  ThirdPartyCaveatID -> revocationCaveatID
}

var revocationMap *util.DirectoryStore
var revocationLock sync.RWMutex

// NewCaveat returns a security.ThirdPartyCaveat for which discharges will be
// issued iff Revoke has not been called for the returned caveat.
func (r *RevocationManager) NewCaveat(dischargerID security.PublicID, dischargerLocation string) (security.ThirdPartyCaveat, error) {
	var revocation [16]byte
	if _, err := rand.Read(revocation[:]); err != nil {
		return nil, err
	}
	restriction := revocationCaveat(revocation)
	cav, err := caveat.NewPublicKeyCaveat(restriction, dischargerID, dischargerLocation, security.ThirdPartyRequirements{})
	if err != nil {
		return nil, err
	}
	if err = r.caveatMap.Put(hex.EncodeToString([]byte(cav.ID())), hex.EncodeToString(revocation[:])); err != nil {
		return nil, err
	}
	return cav, nil
}

// Revoke disables discharges from being issued for the provided third-party caveat.
func (r *RevocationManager) Revoke(caveatID security.ThirdPartyCaveatID) error {
	token, err := r.caveatMap.Get(hex.EncodeToString([]byte(caveatID)))
	if err != nil {
		return err
	}
	return revocationMap.Put(token, string(time.Now().Unix()))
}

// Returns true if the provided caveat has been revoked.
func (r *RevocationManager) IsRevoked(caveatID security.ThirdPartyCaveatID) bool {
	token, err := r.caveatMap.Get(hex.EncodeToString([]byte(caveatID)))
	if err == nil {
		return revocationMap.Exists(token)
	}
	return false
}

type revocationCaveat [16]byte

func (cav revocationCaveat) Validate(security.Context) error {
	revocationLock.RLock()
	if revocationMap == nil {
		revocationLock.RUnlock()
		return fmt.Errorf("missing call to NewRevocationManager")
	}
	revocationLock.RUnlock()
	if revocationMap.Exists(hex.EncodeToString(cav[:])) {
		return fmt.Errorf("revoked")
	}
	return nil
}

// NewRevocationManager returns a RevocationManager that persists information about
// revocationCaveats and allows for revocation and caveat creation.
// This function can only be called once because of the use of global variables.
func NewRevocationManager(dir string) (*RevocationManager, error) {
	revocationLock.Lock()
	defer revocationLock.Unlock()
	if revocationMap != nil {
		return nil, fmt.Errorf("NewRevocationManager can only be called once")
	}
	// If empty string return nil revocationManager
	if len(dir) == 0 {
		return nil, nil
	}
	caveatMap, err := util.NewDirectoryStore(filepath.Join(dir, "caveat_dir"))
	if err != nil {
		return nil, err
	}
	revocationMap, err = util.NewDirectoryStore(filepath.Join(dir, "revocation_dir"))
	if err != nil {
		return nil, err
	}
	return &RevocationManager{caveatMap}, nil
}

func init() {
	vom.Register(revocationCaveat{})
}
