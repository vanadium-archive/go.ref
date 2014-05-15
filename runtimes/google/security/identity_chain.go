package security

// This file describes a certificate chain based implementation of security.PublicID.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"
	"time"

	icaveat "veyron/runtimes/google/security/caveat"
	"veyron/runtimes/google/security/keys"
	"veyron/runtimes/google/security/wire"
	"veyron/security/caveat"

	"veyron2/security"
	"veyron2/vom"
)

// chainPublicID implements security.PublicID.
type chainPublicID struct {
	certificates []wire.Certificate

	// Fields derived from certificates in VomDecode
	publicKey *ecdsa.PublicKey
	rootKey   *ecdsa.PublicKey
	name      string
}

func (id *chainPublicID) Names() []string { return []string{id.String()} }

// Match determines if the PublicID's chained name can be extended to match the
// provided PrincipalPattern. An extension of a chained name is any name obtained
// by joining additional strings to the name using wire.ChainSeparator. Ex: extensions
// of the name "foo/bar" are the names "foo/bar", "foo/bar/baz", "foo/bar/baz/car", and
// so on.
func (id *chainPublicID) Match(pattern security.PrincipalPattern) bool {
	return matchPrincipalPattern(id.String(), pattern)
}

func (id *chainPublicID) PublicKey() *ecdsa.PublicKey { return id.publicKey }

func (id *chainPublicID) String() string {
	// Add a prefix if the identity provider is not trusted.
	if keys.LevelOfTrust(id.rootKey, id.certificates[0].Name) != keys.Trusted {
		return wire.UntrustedIDProviderPrefix + id.name
	}
	return id.name
}

func (id *chainPublicID) VomEncode() (*wire.ChainPublicID, error) {
	return &wire.ChainPublicID{Certificates: id.certificates}, nil
}

func (id *chainPublicID) VomDecode(w *wire.ChainPublicID) error {
	if err := w.VerifyIntegrity(); err != nil {
		return err
	}
	firstKey, err := w.Certificates[0].PublicKey.Decode()
	if err != nil {
		return err
	}
	lastKey, err := w.Certificates[len(w.Certificates)-1].PublicKey.Decode()
	if err != nil {
		return err
	}
	id.name = w.Name()
	id.certificates = w.Certificates
	id.publicKey = lastKey
	id.rootKey = firstKey
	return err
}

// Authorize checks if all caveats on the PublicID validate with respect to the
// provided context and that the identity provider (root public key) is not
// mistrusted. If so returns the original PublicID. This method assumes that
// the existing PublicID was obtained after successfully decoding a serialized
// PublicID and hence has integrity.
func (id *chainPublicID) Authorize(context security.Context) (security.PublicID, error) {
	rootCert := id.certificates[0]
	rootKey, err := rootCert.PublicKey.Decode()
	if err != nil {
		// unlikely to hit this case, as chainPublicID would have integrity.
		return nil, err
	}
	// Implicit "caveat": The identity provider should not be mistrusted.
	switch tl := keys.LevelOfTrust(rootKey, rootCert.Name); tl {
	case keys.Unknown, keys.Trusted:
		// No-op
	default:
		return nil, fmt.Errorf("%v public key(%v) for identity provider %q", tl, rootKey, rootCert.Name)
	}
	for _, c := range id.certificates {
		if err := c.ValidateCaveats(context); err != nil {
			return nil, fmt.Errorf("not authorized because %v", err)
		}
	}
	return id, nil
}

func (id *chainPublicID) ThirdPartyCaveats() (thirdPartyCaveats []security.ServiceCaveat) {
	for _, c := range id.certificates {
		thirdPartyCaveats = append(thirdPartyCaveats, wire.DecodeThirdPartyCaveats(c.Caveats)...)
	}
	return
}

// chainPrivateID implements security.PrivateID
type chainPrivateID struct {
	publicID   *chainPublicID
	privateKey *ecdsa.PrivateKey
}

// PublicID returns the PublicID associated with the PrivateID.
func (id *chainPrivateID) PublicID() security.PublicID { return id.publicID }

// PrivateKey returns the private key associated with the PrivateID.
func (id *chainPrivateID) PrivateKey() *ecdsa.PrivateKey { return id.privateKey }

func (id *chainPrivateID) String() string { return fmt.Sprintf("PrivateID:%v", id.publicID) }

func (id *chainPrivateID) VomEncode() (*wire.ChainPrivateID, error) {
	var err error
	w := &wire.ChainPrivateID{Secret: id.privateKey.D.Bytes()}
	w.PublicID, err = id.publicID.VomEncode()
	return w, err
}

func (id *chainPrivateID) VomDecode(w *wire.ChainPrivateID) error {
	id.publicID = new(chainPublicID)
	if err := id.publicID.VomDecode(w.PublicID); err != nil {
		return err
	}
	id.privateKey = &ecdsa.PrivateKey{
		PublicKey: *id.publicID.publicKey,
		D:         new(big.Int).SetBytes(w.Secret),
	}
	return nil
}

// Bless returns a new PublicID by extending the ceritificate chain of the PrivateID's
// PublicID with a new certificate that has the provided blessingName, caveats, and an
// additional expiry caveat for the given duration.
func (id *chainPrivateID) Bless(blessee security.PublicID, blessingName string, duration time.Duration, caveats []security.ServiceCaveat) (security.PublicID, error) {
	// The integrity of the PublicID blessee is assumed to have been verified
	// (typically by a Vom decode).
	if err := wire.ValidateBlessingName(blessingName); err != nil {
		return nil, err
	}
	cert := wire.Certificate{Name: blessingName}
	if err := cert.PublicKey.Encode(blessee.PublicKey()); err != nil {
		return nil, err
	}
	now := time.Now()
	caveats = append(caveats, security.UniversalCaveat(&caveat.Expiry{IssueTime: now, ExpiryTime: now.Add(duration)}))
	var err error
	if cert.Caveats, err = wire.EncodeCaveats(caveats); err != nil {
		return nil, err
	}
	vomID, err := id.VomEncode()
	if err != nil {
		return nil, err
	}
	if err := cert.Sign(vomID); err != nil {
		return nil, err
	}
	w := &wire.ChainPublicID{
		Certificates: append(id.publicID.certificates, cert),
	}
	return &chainPublicID{
		certificates: w.Certificates,
		publicKey:    blessee.PublicKey(),
		rootKey:      id.publicID.rootKey,
		name:         w.Name(),
	}, nil
}

// Derive returns a new PrivateID that contains the PrivateID's private key and the
// provided PublicID. The provided PublicID must have the same public key as the public
// key of the PrivateID's PublicID.
func (id *chainPrivateID) Derive(pub security.PublicID) (security.PrivateID, error) {
	if !reflect.DeepEqual(pub.PublicKey(), id.publicID.publicKey) {
		return nil, errDeriveMismatch
	}
	chainPub, ok := pub.(*chainPublicID)
	if !ok {
		return nil, fmt.Errorf("PrivateID of type %T cannot be obtained from PublicID of type %T", id, pub)
	}
	return &chainPrivateID{
		publicID:   chainPub,
		privateKey: id.privateKey,
	}, nil
}

func (id *chainPrivateID) MintDischarge(cav security.ThirdPartyCaveat, duration time.Duration, dischargeCaveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	switch c := cav.(type) {
	case *icaveat.PublicKeyCaveat:
		return icaveat.NewPublicKeyDischarge(c, id.privateKey, duration, dischargeCaveats)
	}
	return nil, fmt.Errorf("discharge cannot be constructed for ThirdPartyCaveat of type %T from PrivateID of type %T", cav, id)
}

// newChainPrivateID returns a new PrivateID containing a freshly generated
// private key, and a single self-signed certificate specifying the provided
// name and the public key corresponding to the generated private key.
func newChainPrivateID(name string) (security.PrivateID, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := &chainPrivateID{
		publicID: &chainPublicID{
			certificates: []wire.Certificate{{Name: name}},
			name:         name,
			publicKey:    &key.PublicKey,
			rootKey:      &key.PublicKey,
		},
		privateKey: key,
	}
	cert := &id.publicID.certificates[0]
	if err := cert.PublicKey.Encode(&key.PublicKey); err != nil {
		return nil, err
	}
	vomID, err := id.VomEncode()
	if err != nil {
		return nil, err
	}
	if err := cert.Sign(vomID); err != nil {
		return nil, err
	}
	return id, nil
}

func init() {
	vom.Register(chainPublicID{})
	vom.Register(chainPrivateID{})
}
