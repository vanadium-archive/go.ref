// Package caveat provides some third-party caveat implementations for the Google runtime.
package caveat

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	vcaveat "veyron/security/caveat"
	"veyron2/security"
	"veyron2/security/wire"
)

// nonceLength specifies the length in bytes of the random nonce used
// in PublicKeyCaveat and PublicKeyDischarge.
//
// TODO(ataly, ashankar): Nonce length of 16 bytes was chosen very conservatively.
// The purpose of the random nonce is essentially to separate caveats that have
// the same restriction and validation key (in order to prevent discharge replay).
// Can we use 4bytes instead?.
const nonceLength = 16

// errPublicKeyCaveat returns an error indicating that the provided caveat has
// an invalid or missing discharge.
func errPublicKeyCaveat(c *publicKeyCaveat, err error) error {
	return fmt.Errorf("%v for PublicKeyCaveat{nonce:%v restriction:%q location:%q}", err, c.RandNonce, c.Restriction, c.Location())
}

// publicKeyCaveat implements security.ThirdPartyCaveat. It specifies a restriction,
// a validation key and the location of the third-party responsible for
// discharging the caveat. A discharge for this caveat is a signed assertion whose
// signature can be verified using the validation key.
type publicKeyCaveat struct {
	// RandNonce specifies a cryptographically random nonce (of fixed length) that
	// uniquely identifies the caveat.
	RandNonce []uint8

	// Restriction specifies the predicate to be checked by the dicharging-party.
	Restriction string

	// ValidationKey specifies the public key of the discharging-party.
	ValidationKey wire.PublicKey

	// ThirdPartyLocation specifies the global Veyron name of the discharging-party.
	ThirdPartyLocation string
}

// ID returns a unique 32bytes long identity for the caveat based on the random nonce
// and SHA-256 hash of the restriction embedded in the caveat.
//
// TODO(ataly, ashankar): A 256bit hash is probably much stronger that what we need
// here. Can we truncate the hash to 96bits?
func (c *publicKeyCaveat) ID() security.ThirdPartyCaveatID {
	return security.ThirdPartyCaveatID(id(c.RandNonce, c.Restriction))
}

// Location returns the third-party location embedded in the caveat.
func (c *publicKeyCaveat) Location() string {
	return c.ThirdPartyLocation
}

func (c *publicKeyCaveat) String() string {
	return fmt.Sprintf("PublicKeyCaveat{Restriction: %q, ThirdPartyLocation: %q}", c.Restriction, c.ThirdPartyLocation)
}

// Validate verifies whether the caveat validates for the provided context.
func (c *publicKeyCaveat) Validate(ctx security.Context) error {
	// Check whether the context has a dicharge matching the caveat's ID.
	dis, ok := ctx.CaveatDischarges()[c.ID()]
	if !ok {
		return errPublicKeyCaveat(c, fmt.Errorf("missing discharge"))
	}

	pkDischarge, ok := dis.(*publicKeyDischarge)
	if !ok {
		return errPublicKeyCaveat(c, fmt.Errorf("caveat of type %T cannot be validated with discharge of type %T", c, dis))
	}

	// Validate all caveats present on the discharge.
	for _, cav := range pkDischarge.Caveats {
		if err := cav.Validate(ctx); err != nil {
			return errPublicKeyCaveat(c, err)
		}
	}

	// Check the discharge signature with the validation key from the caveat.
	key, err := c.ValidationKey.Decode()
	if err != nil {
		return errPublicKeyCaveat(c, err)
	}
	var r, s big.Int
	if !ecdsa.Verify(key, pkDischarge.contentHash(), r.SetBytes(pkDischarge.Signature.R), s.SetBytes(pkDischarge.Signature.S)) {
		return errPublicKeyCaveat(c, fmt.Errorf("discharge %v has invalid signature", dis))
	}
	return nil
}

// publicKeyDischarge implements security.ThirdPartyDischarge. It specifies a
// discharge for a publicKeyCaveat, and includes a signature that can be verified
// with the validation key of the caveat. Additionally, the discharge may include
// service caveats which must all be valid in order for the discharge to be
// considered valid.
type publicKeyDischarge struct {
	// RandNonce specifies a cryptographically random nonce that uniquely
	// identifies the discharge. It should be the same as the random nonce
	// of the corresponding caveat.
	RandNonce []uint8

	// Restriction specifies the predicate that was checked before providing
	// this discharge.
	Restriction string

	// Caveats under which this Discharge is valid.
	Caveats []wire.Caveat

	// Signature on the contents of the discharge obtained using the private key
	// corresponding to the validaton key in the caveat.
	Signature wire.Signature
}

// CaveatID returns a unique identity for the discharge based on the random nonce and
// restriction embedded in the discharge.
func (d *publicKeyDischarge) CaveatID() security.ThirdPartyCaveatID {
	return security.ThirdPartyCaveatID(id(d.RandNonce, d.Restriction))
}

func (d *publicKeyDischarge) ThirdPartyCaveats() []security.ServiceCaveat {
	return wire.DecodeThirdPartyCaveats(d.Caveats)
}

// sign uses the provided private key to sign the contents of the discharge. The private
// key typically belongs to the principal that minted the discharge.
func (d *publicKeyDischarge) sign(key *ecdsa.PrivateKey) error {
	r, s, err := ecdsa.Sign(rand.Reader, key, d.contentHash())
	if err != nil {
		return err
	}
	d.Signature.R = r.Bytes()
	d.Signature.S = s.Bytes()
	return nil
}

func (d *publicKeyDischarge) contentHash() []byte {
	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)

	wire.WriteBytes(h, tmp, d.RandNonce)
	wire.WriteString(h, tmp, d.Restriction)

	for _, cav := range d.Caveats {
		wire.WriteString(h, tmp, string(cav.Service))
		wire.WriteBytes(h, tmp, cav.Bytes)
	}
	return h.Sum(nil)
}

// NewPublicKeyCaveat returns a new third-party caveat from the provided restriction,
// third-party identity, and third-party location.
func NewPublicKeyCaveat(restriction string, thirdParty security.PublicID, location string) (security.ThirdPartyCaveat, error) {
	nonce := make([]uint8, nonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	var validationKey wire.PublicKey
	if err := validationKey.Encode(thirdParty.PublicKey()); err != nil {
		return nil, err
	}

	return &publicKeyCaveat{
		RandNonce:          nonce,
		Restriction:        restriction,
		ValidationKey:      validationKey,
		ThirdPartyLocation: location,
	}, nil
}

// NewPublicKeyDischarge returns a new discharge for the provided caveat
// (which should have been created by NewPublicKeyCaveat).
//
// The CaveatID of the discharge is the same as the ID of the caveat, and
// the discharge includes the provided service caveats along with a universal
// expiry caveat for the provided duration. The discharge also includes a
// signature over its contents obtained from the provided private key.
func NewPublicKeyDischarge(caveat security.ThirdPartyCaveat, dischargingKey *ecdsa.PrivateKey, duration time.Duration, caveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	cav, ok := caveat.(*publicKeyCaveat)
	if !ok {
		return nil, fmt.Errorf("cannot mint discharges for %T", caveat)
	}
	discharge := &publicKeyDischarge{
		RandNonce:   cav.RandNonce,
		Restriction: cav.Restriction,
	}

	now := time.Now()
	expiryCaveat := &vcaveat.Expiry{IssueTime: now, ExpiryTime: now.Add(duration)}
	caveats = append(caveats, security.UniversalCaveat(expiryCaveat))

	encodedCaveats, err := wire.EncodeCaveats(caveats)
	if err != nil {
		return nil, err
	}
	discharge.Caveats = encodedCaveats

	if err := discharge.sign(dischargingKey); err != nil {
		return nil, err
	}

	return discharge, nil
}

// id serializes the provided byte slice and SHA-256 hash of the provided string.
func id(b []uint8, str string) string {
	var buf bytes.Buffer
	buf.Write(b)

	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)
	wire.WriteString(h, tmp, str)

	buf.Write(h.Sum(nil))
	return buf.String()
}
