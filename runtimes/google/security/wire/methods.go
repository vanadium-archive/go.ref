package wire

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"veyron/runtimes/google/security/keys"
	"veyron2/security"
	"veyron2/vom"
)

// ErrNoIntegrity is the error returned when the provided PublicID's bytes seem to
// have been manipulated.
var ErrNoIntegrity = errors.New("signature does not match bytes, possible tampering")

// ErrInvalidBlessingName returns an error specifying that the provided blessing name is invalid.
func ErrInvalidBlessingName(blessingName string) error {
	return fmt.Errorf("invalid blessing name:%q", blessingName)
}

// WriteString writes the length and contents of the provided string to the provided Writer.
func WriteString(w io.Writer, tmp []byte, s string) {
	w.Write(tmp[:binary.PutVarint(tmp, int64(len(s)))])
	io.WriteString(w, s)
}

// WriteBytes writes the length and contents of the provided byte slice to the provided Writer.
func WriteBytes(w io.Writer, tmp, b []byte) {
	w.Write(tmp[:binary.PutVarint(tmp, int64(len(b)))])
	w.Write(b)
}

func ellipticCurve(t keyCurve) (elliptic.Curve, error) {
	switch t {
	case keyCurveP256:
		return elliptic.P256(), nil
	default:
		return nil, fmt.Errorf("unrecognized elliptic curve %v", t)
	}
}

// Decode unmarshals the contents of the PublicKey object and returns
// and crypto.ecdsa.PublicKey object.
func (p *PublicKey) Decode() (*ecdsa.PublicKey, error) {
	curve, err := ellipticCurve(p.Curve)
	if err != nil {
		return nil, err
	}
	x, y := elliptic.Unmarshal(curve, p.XY)
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

// Encode takes a crypto.ecdsa.PublicKey object, marshals its contents
// and populates the PublicKey object with them.
func (p *PublicKey) Encode(key *ecdsa.PublicKey) error {
	if key.Curve != elliptic.P256() {
		return fmt.Errorf("unrecognized elliptic curve %T", p.Curve)
	}
	p.Curve = keyCurveP256
	p.XY = elliptic.Marshal(key.Curve, key.X, key.Y)
	return nil
}

// encode serializes a security.Caveat object and sets the resulting bytes on the Caveat object.
func (c *Caveat) encode(caveat security.Caveat) error {
	var b bytes.Buffer
	if err := vom.NewEncoder(&b).Encode(caveat); err != nil {
		return err
	}
	c.Bytes = b.Bytes()
	return nil
}

// EncodeCaveats encodes the provided set of security.ServiceCaveat objects into Caveat objects.
func EncodeCaveats(serviceCaveats []security.ServiceCaveat) ([]Caveat, error) {
	caveats := make([]Caveat, len(serviceCaveats))
	for i, c := range serviceCaveats {
		caveats[i].Service = c.Service
		if err := caveats[i].encode(c.Caveat); err != nil {
			return nil, err
		}
	}
	return caveats, nil
}

// Decode deserializes the contents of the Caveat object to obtain a security.Caveat object.
func (c *Caveat) Decode() (security.Caveat, error) {
	var caveat security.Caveat
	if err := vom.NewDecoder(bytes.NewReader(c.Bytes)).Decode(&caveat); err != nil {
		return nil, err
	}
	return caveat, nil
}

// DecodeThirdPartyCaveats decodes the provided Caveat objects into security.ThirdPartyCaveat
// objects. The resulting objects are wrapped in security.ServiceCaveat objects according
// to the services they are bound to.
func DecodeThirdPartyCaveats(caveats []Caveat) (thirdPartyCaveats []security.ServiceCaveat) {
	for _, wireCav := range caveats {
		cav, err := wireCav.Decode()
		if err != nil {
			continue
		}
		tpCav, ok := cav.(security.ThirdPartyCaveat)
		if !ok {
			continue
		}
		thirdPartyCaveats = append(thirdPartyCaveats, security.ServiceCaveat{Service: wireCav.Service, Caveat: tpCav})
	}
	return
}

// Validate verifies the restriction embedded inside the security.Caveat if the label
// is an empty string (indicating a universal caveat) or if the label matches the Name
// of the LocalID present in the provided context.
func (c *Caveat) Validate(ctx security.Context) error {
	// TODO(ataly): Is checking that the localID matches the caveat's Service pattern
	// the right choice here?
	if c.Service != security.AllPrincipals && (ctx.LocalID() == nil || !ctx.LocalID().Match(security.PrincipalPattern(c.Service))) {
		return nil
	}
	cav, err := c.Decode()
	if err != nil {
		return err
	}
	return cav.Validate(ctx)
}

// -- Helper methods on the wire format for the chain implementation of Identity --

// contentHash returns a SHA256 hash of the contents of the certificate along with the
// provided signature.
func (c *Certificate) contentHash(issuerSignature Signature) []byte {
	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)
	WriteBytes(h, tmp, issuerSignature.R)
	WriteBytes(h, tmp, issuerSignature.S)
	WriteString(h, tmp, c.Name)
	h.Write([]byte{byte(c.PublicKey.Curve)})
	WriteBytes(h, tmp, c.PublicKey.XY)
	binary.Write(h, binary.BigEndian, uint32(len(c.Caveats)))
	for _, cav := range c.Caveats {
		WriteString(h, tmp, string(cav.Service))
		WriteBytes(h, tmp, cav.Bytes)
	}
	return h.Sum(nil)
}

// Sign uses the provided ChainPrivateID to sign the contents of the Certificate.
func (c *Certificate) Sign(issuer *ChainPrivateID) error {
	pubID := issuer.PublicID
	numCerts := len(pubID.Certificates)
	if numCerts == 0 {
		return errors.New("cannot sign with a ChainPrivateID with no certificates")
	}
	pubKey, err := pubID.Certificates[numCerts-1].PublicKey.Decode()
	if err != nil {
		return err
	}
	privKey := &ecdsa.PrivateKey{PublicKey: *pubKey, D: new(big.Int).SetBytes(issuer.Secret)}
	r, s, err := ecdsa.Sign(rand.Reader, privKey, c.contentHash(pubID.Certificates[numCerts-1].Signature))
	if err != nil {
		return err
	}
	c.Signature.R = r.Bytes()
	c.Signature.S = s.Bytes()
	return nil
}

func (c *Certificate) verifyIntegrity(issuerSignature Signature, key *ecdsa.PublicKey) bool {
	var r, s big.Int
	return ecdsa.Verify(key, c.contentHash(issuerSignature), r.SetBytes(c.Signature.R), s.SetBytes(c.Signature.S))
}

// ValidateCaveats verifies if all caveats present on the certificate validate with
// respect to the provided context.
func (c *Certificate) ValidateCaveats(ctx security.Context) error {
	for _, cav := range c.Caveats {
		if err := cav.Validate(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Name returns the chained name obtained by joining all names along the ChainPublicID's
// certificate chain.
func (id *ChainPublicID) Name() string {
	var buf bytes.Buffer
	for i, c := range id.Certificates {
		if i > 0 {
			buf.WriteString(ChainSeparator)
		}
		buf.WriteString(c.Name)
	}
	return buf.String()
}

// VerifyIntegrity verifies that the ChainPublicID has a valid certificate chain, i.e,
// (1) each certificate on the chain has a signature that can be verified using the
// public key specified in the previous certificate, (2) the first certificate's
// signature can be verified using its own public key, and (3) all certificate names
// are valid blessing names.
func (id *ChainPublicID) VerifyIntegrity() error {
	nCerts := len(id.Certificates)
	if nCerts == 0 {
		return ErrNoIntegrity
	}
	verificationKey, err := id.Certificates[0].PublicKey.Decode()
	if err != nil {
		return ErrNoIntegrity
	}
	issuerSignature := Signature{}
	for _, c := range id.Certificates {
		if err := ValidateBlessingName(c.Name); err != nil {
			return err
		}
		// TODO(ashankar, ataly): Do we worry about timing attacks by
		// early exiting on invalid certificate?
		if !c.verifyIntegrity(issuerSignature, verificationKey) {
			return ErrNoIntegrity
		}
		if verificationKey, err = c.PublicKey.Decode(); err != nil {
			return ErrNoIntegrity
		}
		issuerSignature = c.Signature
	}
	return nil
}

// -- Helper methods on the wire format for the chain implementation of Identity --

// contentHash returns a SHA256 hash of the contents of the blessing and the provided
// public key.
func (b *Blessing) contentHash(pubKey PublicKey) []byte {
	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)
	h.Write([]byte{byte(pubKey.Curve)})
	WriteBytes(h, tmp, pubKey.XY)
	WriteString(h, tmp, b.Name)
	for _, cav := range b.Caveats {
		WriteString(h, tmp, string(cav.Service))
		WriteBytes(h, tmp, cav.Bytes)
	}
	return h.Sum(nil)
}

// Sign uses the provided private key to sign the contents of the Blessing along with
// the provided public key. The private key typically belongs to the principal providing
// the blessing and the public key to the principal being blessed.
func (b *Blessing) Sign(pubKey PublicKey, key *ecdsa.PrivateKey) error {
	r, s, err := ecdsa.Sign(rand.Reader, key, b.contentHash(pubKey))
	if err != nil {
		return err
	}
	b.Signature.R = r.Bytes()
	b.Signature.S = s.Bytes()
	return nil
}

func (b *Blessing) verifyIntegrity(pubKey PublicKey) bool {
	var key *ecdsa.PublicKey
	var err error
	if b.Blessor == nil {
		// This is a leaf blessing.
		key, err = pubKey.Decode()
	} else {
		key, err = b.Blessor.PublicKey.Decode()
	}
	if err != nil {
		return false
	}
	var r, s big.Int
	return ecdsa.Verify(key, b.contentHash(pubKey), r.SetBytes(b.Signature.R), s.SetBytes(b.Signature.S))
}

// validateCaveats verifies if all caveats present on the blessing validate with
// respect to the provided context.
func (b *Blessing) validateCaveats(ctx security.Context) error {
	for _, cav := range b.Caveats {
		if err := cav.Validate(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Names returns an array of blessing-chains and associated identity provider's
// public key (i.e., the public key of the leaf blessing). More formally,
//
// names[i] is the list of names of all blessings on a path from a leaf
// blessing (signed by providers[i]) to the public key of id.
func (id *TreePublicID) Names() (names [][]string, providers []*ecdsa.PublicKey) {
	for _, b := range id.Blessings {
		if b.Blessor == nil {
			// leaf blessing - id is an identity provider for this blessing.
			names = append(names, []string{b.Name})
			key, _ := id.PublicKey.Decode()
			providers = append(providers, key)
			continue
		}
		n2, p2 := b.Blessor.Names()
		providers = append(providers, p2...)
		for _, n := range n2 {
			names = append(names, append(n, b.Name))
		}
	}
	return
}

// ThirdPartyCaveats returns the set of third-party restrictions embedded a TreePublicID.
// The resulting objects are wrapped in security.ServiceCaveat objects according to the
// services they are bound to.
func (id *TreePublicID) ThirdPartyCaveats() (thirdPartyCaveats []security.ServiceCaveat) {
	for _, b := range id.Blessings {
		thirdPartyCaveats = append(thirdPartyCaveats, DecodeThirdPartyCaveats(b.Caveats)...)
		if b.Blessor != nil {
			thirdPartyCaveats = append(thirdPartyCaveats, b.Blessor.ThirdPartyCaveats()...)
		}
	}
	return
}

// VerifyIntegrity verifies that the TreePublicID has a valid blessing tree, i.e,
// (1) each blessing on the tree has a signature that can be verified using the
// public key specified in its child node, (2) the leaf blessing's signature can
// be verified using the public key that is being blessed by it, and (3) all
// blessing names are valid.
func (id *TreePublicID) VerifyIntegrity() error {
	if len(id.Blessings) == 0 {
		return ErrNoIntegrity
	}
	for _, b := range id.Blessings {
		if err := ValidateBlessingName(b.Name); err != nil {
			return err
		}
		if !b.verifyIntegrity(id.PublicKey) {
			return ErrNoIntegrity
		}
		// TODO(ashankar, ataly): Do we worry about timing attacks by
		// early exiting on invalid blessing?
		if b.Blessor == nil {
			continue
		}
		if err := b.Blessor.VerifyIntegrity(); err != nil {
			return ErrNoIntegrity
		}
	}
	return nil
}

// Authorize returns a new TreePublicID with just those blessing chains from the existing
// PublicID whose caveats validate with respect to the provided context and whose leaf
// public key is trusted. This method assumes that the existing TreePublicID has
// integrity (see VerifyIntegrity).
func (id *TreePublicID) Authorize(ctx security.Context) *TreePublicID {
	authorizedID := &TreePublicID{PublicKey: id.PublicKey}
	for _, b := range id.Blessings {
		if b.validateCaveats(ctx) != nil {
			continue
		}
		bID := b.Blessor
		if bID == nil {
			// This is a leaf blessing.
			key, err := id.PublicKey.Decode()
			if err != nil {
				continue
			}
			switch keys.LevelOfTrust(key, b.Name) {
			case keys.Unknown, keys.Trusted:
				// No-op
			default:
				// Mistrusted blessings are silently dropped.
				continue
			}
		} else if bID = b.Blessor.Authorize(ctx); bID == nil {
			continue
		}
		authorizedID.Blessings = append(authorizedID.Blessings, Blessing{
			Blessor:   bID,
			Name:      b.Name,
			Caveats:   b.Caveats,
			Signature: b.Signature,
		})
	}
	if authorizedID.Blessings == nil {
		return nil
	}
	return authorizedID
}

// ValidateBlessingName verifies if the provided name is fit to be the name of a blessing.
func ValidateBlessingName(name string) error {
	// TODO(ataly, ashankar): Define the list of reserved characters (such as  "*", "#",
	// "/", "\", etc.) and ensure that the check below ensures absence of all of them.
	if name == "" || strings.ContainsAny(name, ChainSeparator) {
		return ErrInvalidBlessingName(name)
	}
	return nil
}
