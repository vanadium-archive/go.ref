package caveat

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"veyron/runtimes/google/security/wire"

	"veyron2/security"
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
func errPublicKeyCaveat(c *PublicKeyCaveat, err error) error {
	return fmt.Errorf("%v for PublicKeyCaveat{nonce:%v restriction:%q location:%q}", err, c.RandNonce, c.Restriction, c.Location())
}

// PublicKeyCaveat implements security.ThirdPartyCaveat. It specifies a restriction,
// a validation key and the location of the third-party responsible for
// discharging the caveat. A discharge for this caveat is a signed assertion whose
// signature can be verified using the validation key.
type PublicKeyCaveat struct {
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
func (c *PublicKeyCaveat) ID() security.ThirdPartyCaveatID {
	return security.ThirdPartyCaveatID(id(c.RandNonce, c.Restriction))
}

// Location returns the third-party location embedded in the caveat.
func (c *PublicKeyCaveat) Location() string {
	return c.ThirdPartyLocation
}

func (c *PublicKeyCaveat) String() string {
	return fmt.Sprintf("PublicKeyCaveat{Restriction: %q, ThirdPartyLocation: %q}", c.Restriction, c.ThirdPartyLocation)
}

// Validate verifies whether the caveat validates for the provided context.
func (c *PublicKeyCaveat) Validate(ctx security.Context) error {
	// Check whether the context has a dicharge matching the caveat's ID.
	dis, ok := ctx.CaveatDischarges()[c.ID()]
	if !ok {
		return errPublicKeyCaveat(c, fmt.Errorf("missing discharge"))
	}

	pkDischarge, ok := dis.(*PublicKeyDischarge)
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

// PublicKeyDischarge implements security.ThirdPartyDischarge. It specifies a
// discharge for a PublicKeyCaveat, and includes a signature that can be verified
// with the validation key of the caveat. Additionally, the discharge may include
// service caveats which must all be valid in order for the discharge to be
// considered valid.
type PublicKeyDischarge struct {
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
func (d *PublicKeyDischarge) CaveatID() security.ThirdPartyCaveatID {
	return security.ThirdPartyCaveatID(id(d.RandNonce, d.Restriction))
}

func (d *PublicKeyDischarge) ThirdPartyCaveats() []security.ServiceCaveat {
	return wire.DecodeThirdPartyCaveats(d.Caveats)
}
