package security

// This file describes a blessing tree based implementation of security.PublicID.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"time"

	icaveat "veyron/runtimes/google/security/caveat"
	"veyron/runtimes/google/security/keys"
	"veyron/runtimes/google/security/wire"
	"veyron/security/caveat"
	"veyron2/security"
	"veyron2/vom"
)

// nameSeparator is used to join blessing chain names to form a blessing tree name.
const nameSeparator = "#"

// treePublicID implements security.PublicID.
type treePublicID struct {
	blessings []wire.Blessing
	publicKey *ecdsa.PublicKey
	paths     []treePath
}

func (id *treePublicID) Names() []string {
	var names []string
	for _, p := range id.paths {
		names = append(names, p.String())
	}
	return names
}

// Match determines if the PublicID's chained name can be extended to match the
// provided PrincipalPattern. An extension of a chained name is any name obtained
// by joining additional strings to the name using wire.ChainSeparator. Ex: extensions
// of the name "foo/bar" are the names "foo/bar", "foo/bar/baz", "foo/bar/baz/car", and
// so on.
func (id *treePublicID) Match(pattern security.PrincipalPattern) bool {
	for _, p := range id.paths {
		if matchPrincipalPattern(p.String(), pattern) {
			return true
		}
	}
	return false
}

func (id *treePublicID) PublicKey() *ecdsa.PublicKey { return id.publicKey }

func (id *treePublicID) String() string { return strings.Join(id.Names(), nameSeparator) }

func (id *treePublicID) VomEncode() (*wire.TreePublicID, error) {
	var pKey wire.PublicKey
	if err := pKey.Encode(id.publicKey); err != nil {
		return nil, err
	}
	return &wire.TreePublicID{Blessings: id.blessings, PublicKey: pKey}, nil
}

func (id *treePublicID) VomDecode(w *wire.TreePublicID) error {
	if err := w.VerifyIntegrity(); err != nil {
		return err
	}
	return id.initFromWire(w)
}

func (id *treePublicID) initFromWire(w *wire.TreePublicID) error {
	names, providers := w.Names()
	paths := make([]treePath, len(names))
	for i, p := range paths {
		if len(names[i]) == 0 || providers[i] == nil {
			return fmt.Errorf("invalid identity provider: (name=%q, key=%v)", names[i], providers[i])
		}
		p.name = strings.Join(names[i], wire.ChainSeparator)
		p.providerName = names[i][0]
		p.providerKey = providers[i]
		paths[i] = p
	}
	key, err := w.PublicKey.Decode()
	if err != nil {
		return err
	}
	id.blessings = w.Blessings
	id.publicKey = key
	id.paths = paths
	return nil
}

// Authorize returns a new treePublicID with only those blessing chains from
// the existing PublicID whose caveats validate with respect to the provided
// context and whose identity providers (leaf blessings) are trusted.  This
// method assumes that the existing PublicID was obtained after successfully
// decoding a serialized PublicID and hence has integrity.
func (id *treePublicID) Authorize(context security.Context) (security.PublicID, error) {
	w, err := id.VomEncode()
	if err != nil {
		return nil, err
	}
	wAuthorizedID := w.Authorize(context)
	if wAuthorizedID == nil {
		return nil, errors.New("tree identity with no blessings")
	}
	authorizedID := &treePublicID{}
	if err := authorizedID.initFromWire(wAuthorizedID); err != nil {
		return nil, err
	}
	return authorizedID, nil
}

// treePath encapsulates the name created by following blessings that link the
// public key of a treePublicID to an identity provider.
type treePath struct {
	name         string
	providerName string
	providerKey  *ecdsa.PublicKey
}

func (t *treePath) String() string {
	if keys.LevelOfTrust(t.providerKey, t.providerName) != keys.Trusted {
		return wire.UntrustedIDProviderPrefix + t.name
	}
	return t.name
}

func (id *treePublicID) ThirdPartyCaveats() (thirdPartyCaveats []security.ServiceCaveat) {
	if w, err := id.VomEncode(); err == nil {
		thirdPartyCaveats = w.ThirdPartyCaveats()
	}
	return
}

// treePrivateID implements security.PrivateID.
type treePrivateID struct {
	publicID   *treePublicID
	privateKey *ecdsa.PrivateKey
}

// PublicID returns the PublicID associated with the PrivateID.
func (id *treePrivateID) PublicID() security.PublicID { return id.publicID }

// PrivateKey returns the private key associated with the PrivateID.
func (id *treePrivateID) PrivateKey() *ecdsa.PrivateKey { return id.privateKey }

func (id *treePrivateID) String() string { return fmt.Sprintf("PrivateID:%v", id.publicID) }

func (id *treePrivateID) VomEncode() (*wire.TreePrivateID, error) {
	var err error
	w := &wire.TreePrivateID{Secret: id.privateKey.D.Bytes()}
	w.PublicID, err = id.publicID.VomEncode()
	return w, err
}

func (id *treePrivateID) VomDecode(w *wire.TreePrivateID) error {
	if err := id.publicID.VomDecode(w.PublicID); err != nil {
		return err
	}
	id.privateKey = &ecdsa.PrivateKey{
		PublicKey: *id.publicID.publicKey,
		D:         new(big.Int).SetBytes(w.Secret),
	}
	return nil
}

// Bless returns a new PublicID by extendinig the blessee's blessings with a
// new one signed by id's private key and named blessingName with caveats and
// an additional expiry caveat for the provided duration.
func (id *treePrivateID) Bless(blesseeID security.PublicID, blessingName string, duration time.Duration, caveats []security.ServiceCaveat) (security.PublicID, error) {
	// The integrity of the PublicID blessee is assumed to have been verified
	// (typically by a Vom decode)
	if err := wire.ValidateBlessingName(blessingName); err != nil {
		return nil, err
	}
	blessee, ok := blesseeID.(*treePublicID)
	if !ok {
		return nil, fmt.Errorf("PrivateID of type %T cannot bless PublicID of type %T", id, blesseeID)
	}
	if blessee.blessings == nil {
		return nil, errors.New("blessee must at least have one blessing before it can be blessed further")
	}

	blessing := wire.Blessing{Name: blessingName}
	var err error
	if blessing.Blessor, err = id.publicID.VomEncode(); err != nil {
		return nil, err
	}
	now := time.Now()
	caveats = append(caveats, security.UniversalCaveat(&caveat.Expiry{IssueTime: now, ExpiryTime: now.Add(duration)}))
	if blessing.Caveats, err = wire.EncodeCaveats(caveats); err != nil {
		return nil, err
	}
	var pKey wire.PublicKey
	if err := pKey.Encode(blessee.publicKey); err != nil {
		return nil, err
	}
	if err := blessing.Sign(pKey, id.privateKey); err != nil {
		return nil, err
	}
	w := &wire.TreePublicID{
		Blessings: append(blessee.blessings, blessing),
		PublicKey: pKey,
	}
	blessed := &treePublicID{}
	if err := blessed.initFromWire(w); err != nil {
		return nil, err
	}
	return blessed, nil
}

// Derive returns a new PrivateID containing priv's private key and the provided PublicID.
// The provided PublicID must have the same public key as the public key of priv's PublicID.
func (id *treePrivateID) Derive(pub security.PublicID) (security.PrivateID, error) {
	if !reflect.DeepEqual(pub.PublicKey(), id.publicID.publicKey) {
		return nil, errDeriveMismatch
	}
	treePub, ok := pub.(*treePublicID)
	if !ok {
		return nil, fmt.Errorf("PrivateID of type %T cannot be obtained from PublicID of type %T", id, pub)
	}
	return &treePrivateID{
		publicID:   treePub,
		privateKey: id.privateKey,
	}, nil

}

func (id *treePrivateID) MintDischarge(cav security.ThirdPartyCaveat, duration time.Duration, dischargeCaveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	switch c := cav.(type) {
	case *icaveat.PublicKeyCaveat:
		return icaveat.NewPublicKeyDischarge(c, id.privateKey, duration, dischargeCaveats)
	}
	return nil, fmt.Errorf("discharge cannot be constructed for ThirdPartyCaveat of type %T from PrivateID of type %T", cav, id)
}

// newTreePrivateID returns a new PrivateID containing a freshly generated private
// key, and a single self-signed blessing for the provided name and the public key
// corresponding to the generated private key.
func newTreePrivateID(name string) (security.PrivateID, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	var pKey wire.PublicKey
	if err := pKey.Encode(&key.PublicKey); err != nil {
		return nil, err
	}
	blessing := wire.Blessing{Name: name}
	if err := blessing.Sign(pKey, key); err != nil {
		return nil, err
	}
	w := &wire.TreePublicID{Blessings: []wire.Blessing{blessing}, PublicKey: pKey}
	publicID := &treePublicID{}
	if err := publicID.initFromWire(w); err != nil {
		return nil, err
	}
	return &treePrivateID{publicID: publicID, privateKey: key}, nil
}

func init() {
	vom.Register(treePublicID{})
	vom.Register(treePrivateID{})
}
