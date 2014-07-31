package discharger

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"veyron/security/caveat"
	ssecurity "veyron/services/security"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vlog"
	"veyron2/vom"
)

// TODO(ataly, andreser) This package uses a global variable to store the
// revoker state to make it accessible to caveat.Validate. Ideally, we would
// pass this in through the context (or something equivalent).

type revocationServiceT struct {
	store       storage.Store
	pathInStore string
}

var revocationService struct {
	*revocationServiceT
	sync.Mutex
}

type revocationCaveat [32]byte

func (cav revocationCaveat) Validate(security.Context) error {
	// TODO(ashankar,mattr): Figure out how to get the context of an existing RPC here
	rctx := rt.R().NewContext()
	revocation := revocationService.store.BindObject(
		naming.Join(revocationService.pathInStore, hex.EncodeToString(cav[:])))
	exists, err := revocation.Exists(rctx)
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
	restriction := revocationCaveat(sha256.Sum256(revocation[:]))
	cav, err := caveat.NewPublicKeyCaveat(restriction, dischargerID, dischargerLocation)
	return revocation, cav, err
}

func (revoceationService *revocationServiceT) Revoke(ctx ipc.ServerContext, caveatPreimage ssecurity.RevocationToken) error {
	caveatNonce := sha256.Sum256(caveatPreimage[:])
	revocation := revocationService.store.BindObject(naming.Join(revocationService.pathInStore, hex.EncodeToString(caveatNonce[:])))
	if _, err := revocation.Put(ctx, caveatPreimage[:]); err != nil {
		return err
	}
	return nil
}

// NewRevoker returns a new revoker service that can be passed to a dispatcher.
// Currently, due to the use of global variables, this function can be called only once.
func NewRevoker(storeName, pathInStore string) (interface{}, error) {
	revocationService.Lock()
	defer revocationService.Unlock()
	if revocationService.revocationServiceT != nil {
		return nil, fmt.Errorf("revoker.Revoker called more than once")
	}
	var err error
	revocationService.revocationServiceT = new(revocationServiceT)
	revocationService.store, err = vstore.New(storeName)
	if err != nil {
		return nil, err
	}

	rctx := rt.R().NewContext()
	// Transaction is rooted at "", so tname == tid.
	tname, err := revocationService.store.BindTransactionRoot("").CreateTransaction(rctx)
	if err != nil {
		return nil, err
	}

	// Create parent directories for the revoker root, if necessary
	// TODO(tilaks,andreser): provide a `mkdir -p` equivalent in store
	l := strings.Split(pathInStore, "/")
	fmt.Println(l)
	for i := 0; i <= len(l); i++ {
		fmt.Println(i, filepath.Join(l[:i]...))
		prefix := filepath.Join(l[:i]...)
		o := revocationService.store.BindObject(naming.Join(tname, prefix))
		if exist, err := o.Exists(rctx); err != nil {
			vlog.Infof("Error checking existence at %q: %s", prefix, err)
		} else if !exist {
			if _, err := o.Put(rctx, &Dir{}); err != nil {
				vlog.Infof("Error creating directory %q: %s", prefix, err)
			}
		}
	}
	if err := revocationService.store.BindTransaction(tname).Commit(rctx); err != nil {
		vlog.Fatalf("Failed to commit creation of revoker root at %s: %s", pathInStore, err)
	}
	return ssecurity.NewServerRevoker(revocationService.revocationServiceT), nil
}

func init() {
	vom.Register(revocationCaveat{})
}
