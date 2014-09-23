package discharger

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	ssecurity "veyron.io/veyron/veyron/services/security"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

// The revocation caveats will be stored in a directory with a file for each.
type revocationDir string

// Returns whether the caveat exists in the recovation file.
func (dir revocationDir) exists(cav string) (bool, error) {
	if len(dir) == 0 {
		return false, fmt.Errorf("missing call to NewRecocationCaveat")
	}
	_, err := os.Stat(filepath.Join(string(dir), cav))
	return !os.IsNotExist(err), nil
}

func (dir revocationDir) put(caveatNonce string, caveatPreimage []byte) error {
	if len(dir) == 0 {
		return fmt.Errorf("missing call to NewRevoker")
	}
	return ioutil.WriteFile(filepath.Join(string(dir), caveatNonce), caveatPreimage, 0600)
}

func (dir revocationDir) Revoke(ctx ipc.ServerContext, caveatPreimage ssecurity.RevocationToken) error {
	if len(dir) == 0 {
		return fmt.Errorf("missing call to NewRevoker")
	}
	caveatNonce := sha256.Sum256(caveatPreimage[:])
	return revocationService.put(hex.EncodeToString(caveatNonce[:]), caveatPreimage[:])
}

var revocationService struct {
	revocationDir
	sync.Mutex
}

type revocationCaveat [32]byte

func (cav revocationCaveat) Validate(security.Context) error {
	exists, err := revocationService.exists(hex.EncodeToString(cav[:]))
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("revoked")
	}
	return nil
}

// NewRevocationCaveat returns a security.ThirdPartyCaveat that discharger will
// mint discharges until explicitly told not to by calling Revoke on it
// (using the returned revocation token)
func NewRevocationCaveat(dischargerID security.PublicID, dischargerLocation string) (ssecurity.RevocationToken, security.ThirdPartyCaveat, error) {
	var revocation ssecurity.RevocationToken
	if _, err := rand.Read(revocation[:]); err != nil {
		return revocation, nil, err
	}
	restriction, err := security.NewCaveat(revocationCaveat(sha256.Sum256(revocation[:])))
	if err != nil {
		return revocation, nil, err
	}
	cav, err := security.NewPublicKeyCaveat(dischargerID.PublicKey(), dischargerLocation, security.ThirdPartyRequirements{}, restriction)
	return revocation, cav, err
}

// NewRevoker returns a revoker service implementation that persists information about revocations on the filesystem in dir.
// Can only be called once due to global variables.
func NewRevoker(dir string) (ssecurity.RevokerService, error) {
	revocationService.Lock()
	defer revocationService.Unlock()
	if len(revocationService.revocationDir) > 0 {
		return nil, fmt.Errorf("revoker.Revoker called more than once.")
	}
	revocationService.revocationDir = revocationDir(dir)

	// create the directory if not already there.
	os.MkdirAll(dir, 0700)

	return revocationService.revocationDir, nil
}

func init() {
	vom.Register(revocationCaveat{})
}
