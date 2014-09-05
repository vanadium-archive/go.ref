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

	"veyron/runtimes/google/security/keys"
	"veyron/security/caveat"
	"veyron/security/signing"

	"veyron2/security"
	"veyron2/security/wire"
	"veyron2/vom"
)

const (
	// unknownIDProviderPrefix is the prefix added when stringifying
	// an identity for which there is no entry for the root certificate
	// in the trusted keys set.
	unknownIDProviderPrefix = "unknown/"
	// mistrustedIDProviderPrefix is the prefix added when stringifying
	// an identity whose root certificate has a public key that does
	// not exist in the (non-empty) set of trusted keys for that root.
	mistrustedIDProviderPrefix = "mistrusted/"
)

// chainPublicID implements security.PublicID.
type chainPublicID struct {
	certificates []wire.Certificate

	// Fields derived from certificates in VomDecode
	publicKey security.PublicKey
	rootKey   security.PublicKey
	name      string
}

func (id *chainPublicID) Names() []string {
	// Return a name only if the identity provider is trusted.
	if keys.LevelOfTrust(id.rootKey, id.certificates[0].Name) == keys.Trusted {
		return []string{id.name}
	}
	return nil
}

func (id *chainPublicID) PublicKey() security.PublicKey { return id.publicKey }

func (id *chainPublicID) String() string {
	// Add a prefix if the identity provider is not trusted.
	switch keys.LevelOfTrust(id.rootKey, id.certificates[0].Name) {
	case keys.Trusted:
		return id.name
	case keys.Mistrusted:
		return mistrustedIDProviderPrefix + id.name
	default:
		return unknownIDProviderPrefix + id.name
	}
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
// provided context and if so returns the original PublicID. This method assumes that
// the existing PublicID was obtained after successfully decoding a serialized
// PublicID and hence has integrity.
func (id *chainPublicID) Authorize(context security.Context) (security.PublicID, error) {
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
	security.Signer
	publicID   *chainPublicID
	privateKey *ecdsa.PrivateKey // can be nil
}

func (id *chainPrivateID) PublicID() security.PublicID { return id.publicID }

func (id *chainPrivateID) String() string { return fmt.Sprintf("PrivateID:%v", id.publicID) }

func (id *chainPrivateID) VomEncode() (*wire.ChainPrivateID, error) {
	if id.privateKey == nil {
		// TODO(ataly): Figure out a clean way to serialize Signers.
		return nil, fmt.Errorf("cannot vom-encode a chainPrivateID that doesn't have access to a private key")
	}
	pub, err := id.publicID.VomEncode()
	if err != nil {
		return nil, err
	}
	return &wire.ChainPrivateID{Secret: id.privateKey.D.Bytes(), PublicID: *pub}, nil
}

func (id *chainPrivateID) VomDecode(w *wire.ChainPrivateID) error {
	id.publicID = new(chainPublicID)
	if err := id.publicID.VomDecode(&w.PublicID); err != nil {
		return err
	}
	id.privateKey = &ecdsa.PrivateKey{
		PublicKey: *id.publicID.publicKey.DO_NOT_USE(),
		D:         new(big.Int).SetBytes(w.Secret),
	}
	id.Signer = signing.NewClearSigner(id.privateKey)
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
	caveats = append(caveats, caveat.UniversalCaveat(&caveat.Expiry{IssueTime: now, ExpiryTime: now.Add(duration)}))
	var err error
	if cert.Caveats, err = wire.EncodeCaveats(caveats); err != nil {
		return nil, err
	}
	vomPubID, err := id.publicID.VomEncode()
	if err != nil {
		return nil, err
	}
	if err := cert.Sign(id, vomPubID); err != nil {
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

func (id *chainPrivateID) Derive(pub security.PublicID) (security.PrivateID, error) {
	if !reflect.DeepEqual(pub.PublicKey(), id.publicID.publicKey) {
		return nil, errDeriveMismatch
	}
	switch p := pub.(type) {
	case *chainPublicID:
		return &chainPrivateID{
			Signer:     id.Signer,
			publicID:   p,
			privateKey: id.privateKey,
		}, nil
	case *setPublicID:
		privs := make([]security.PrivateID, len(*p))
		var err error
		for ix, ip := range *p {
			if privs[ix], err = id.Derive(ip); err != nil {
				return nil, fmt.Errorf("Derive failed for public id %d of %d in set: %v", ix, len(*p), err)
			}
		}
		return setPrivateID(privs), nil
	default:
		return nil, fmt.Errorf("PrivateID of type %T cannot be used to Derive from PublicID of type %T", id, pub)
	}
}

func (id *chainPrivateID) MintDischarge(cav security.ThirdPartyCaveat, ctx security.Context, duration time.Duration, dischargeCaveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	return caveat.NewPublicKeyDischarge(id, cav, ctx, duration, dischargeCaveats)
}

// newChainPrivateID returns a new PrivateID that uses the provided Signer to generate
// signatures.  The returned PrivateID additionaly contains a single self-signed
// certificate with the given name.
//
// If a nil signer is provided, this method will generate a new public/private key pair
// and use a system-default signer, which stores the private key in the clear in the memory
// of the running process.
func newChainPrivateID(name string, signer security.Signer) (security.PrivateID, error) {
	if err := wire.ValidateBlessingName(name); err != nil {
		return nil, err
	}
	var privKey *ecdsa.PrivateKey
	if signer == nil {
		var err error
		privKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		signer = signing.NewClearSigner(privKey)
	}

	id := &chainPrivateID{
		Signer: signer,
		publicID: &chainPublicID{
			certificates: []wire.Certificate{{Name: name}},
			name:         name,
			publicKey:    signer.PublicKey(),
			rootKey:      signer.PublicKey(),
		},
		privateKey: privKey,
	}
	// Self-sign the (single) certificate.
	cert := &id.publicID.certificates[0]
	if err := cert.PublicKey.Encode(signer.PublicKey()); err != nil {
		return nil, err
	}
	vomPubID, err := id.publicID.VomEncode()
	if err != nil {
		return nil, err
	}
	if err := cert.Sign(id, vomPubID); err != nil {
		return nil, err
	}
	return id, nil
}

func init() {
	vom.Register(chainPublicID{})
	vom.Register(chainPrivateID{})
}
