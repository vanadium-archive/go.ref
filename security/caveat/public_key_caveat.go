// Package caveat will be removed as the new security API takes shape before November 1, 2014.
package caveat

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	vsecurity "veyron/security"

	"veyron2/security"
	"veyron2/security/wire"
	"veyron2/vom"
)

// nonceLength specifies the length in bytes of the random nonce used
// in publicKeyCaveat and publicKeyDischarge.
//
// TODO(ataly, ashankar): Nonce length of 16 bytes was chosen very conservatively.
// The purpose of the random nonce is essentially to separate caveats that have
// the same restriction and validation key (in order to prevent discharge replay).
// Can we use 4bytes instead?.
const nonceLength = 16

// publicKeyCaveat implements security.ThirdPartyCaveat. It specifies a restriction,
// a validation key and the location (object name) of the third-party responsible
// for discharging the caveat. A discharge for this caveat is a signed assertion whose
// signature can be verified using the validation key.
//
// TODO(ataly): This should be a VDL defined type.
type publicKeyCaveat struct {
	// RandNonce specifies a cryptographically random nonce (of fixed length) that
	// uniquely identifies the caveat.
	RandNonce []uint8
	// DischargeMintingCaveat specifies the caveat that has to be validated
	// before minting a discharge for a publicKeyCaveat.
	DischargeMintingCaveat security.Caveat
	// ValidationKey specifies the public key of the discharging-party.
	ValidationKey wire.PublicKey
	// ThirdPartyLocation specifies the object name of the discharging-party.
	ThirdPartyLocation string
	// ThirdPartyRequirements specify the information wanted in order to issue a discharge.
	ThirdPartyRequirements security.ThirdPartyRequirements
}

// ID returns a unique 32bytes long identity for the caveat based on the random nonce
// and SHA-256 hash of the restriction embedded in the caveat.
//
// TODO(ataly, ashankar): A 256bit hash is probably much stronger that what we need
// here. Can we truncate the hash to 96bits?
func (c *publicKeyCaveat) ID() string {
	return id(c.RandNonce, c.DischargeMintingCaveat.ValidatorVOM)
}

func (c *publicKeyCaveat) Location() string {
	return c.ThirdPartyLocation
}

func (c *publicKeyCaveat) String() string {
	return fmt.Sprintf("publicKeyCaveat{DischargeMintingCaveat: (%v bytes), ThirdPartyLocation: %q}", len(c.DischargeMintingCaveat.ValidatorVOM), c.ThirdPartyLocation)
}

func (c *publicKeyCaveat) Requirements() security.ThirdPartyRequirements {
	return c.ThirdPartyRequirements
}

func (c *publicKeyCaveat) Validate(ctx security.Context) error {
	// Check whether the context has a dicharge matching the caveat's ID.
	dis, ok := ctx.Discharges()[c.ID()]
	if !ok {
		return fmt.Errorf("missing discharge")
	}

	pkDischarge, ok := dis.(*publicKeyDischarge)
	if !ok {
		return fmt.Errorf("caveat of type %T cannot be validated with discharge of type %T", c, dis)
	}

	// Validate all caveats present on the discharge.
	validators, err := vsecurity.CaveatValidators(pkDischarge.Caveats...)
	if err != nil {
		return err
	}
	for _, v := range validators {
		if err := v.Validate(ctx); err != nil {
			return err
		}
	}

	// Check the discharge signature with the validation key from the caveat.
	key, err := c.ValidationKey.Decode()
	if err != nil {
		return err
	}
	if !pkDischarge.Signature.Verify(key, pkDischarge.contentHash()) {
		return fmt.Errorf("discharge %v has invalid signature", dis)
	}
	return nil
}

// publicKeyDischarge implements security.Discharge. It specifies a discharge
// for a publicKeyCaveat, and includes a signature that can be verified with
// the validation key of the publicKeyCaveat. Additionally, the discharge may
// also include caveats which must all validate in order for the discharge to
// be considered valid.
//
// TODO(ataly): This should be a VDL-defined type?
type publicKeyDischarge struct {
	// CaveatID is used to match this Discharge to the the ThirdPartyCaveat it is for.
	CaveatID string

	// Caveats under which this Discharge is valid.
	Caveats []security.Caveat

	// Signature on the contents of the discharge that can be verified using the
	// validaton key in the publicKeycaveat this discharge is for.
	Signature security.Signature
}

func (d *publicKeyDischarge) ID() string {
	return d.CaveatID
}

func (d *publicKeyDischarge) ThirdPartyCaveats() []security.ThirdPartyCaveat {
	return vsecurity.ThirdPartyCaveats(d.Caveats...)
}

func (d *publicKeyDischarge) sign(signer security.PrivateID) (err error) {
	d.Signature, err = signer.Sign(d.contentHash())
	return
}

func (d *publicKeyDischarge) contentHash() []byte {
	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)

	wire.WriteString(h, tmp, d.CaveatID)
	for _, cav := range d.Caveats {
		wire.WriteBytes(h, tmp, cav.ValidatorVOM)
	}
	return h.Sum(nil)
}

// NewPublicKeyCaveat returns a security.ThirdPartyCaveat which requires a
// discharge from a principal identified by the public key 'key' and present
// at the object name 'location'. This discharging principal is expected to
// validate 'caveat' before issuing a discharge.
func NewPublicKeyCaveat(caveat security.Caveat, key security.PublicKey, location string, requirements security.ThirdPartyRequirements) (security.ThirdPartyCaveat, error) {
	nonce := make([]uint8, nonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	var validationKey wire.PublicKey
	if err := validationKey.Encode(key); err != nil {
		return nil, err
	}

	return &publicKeyCaveat{
		RandNonce:              nonce,
		DischargeMintingCaveat: caveat,
		ValidationKey:          validationKey,
		ThirdPartyLocation:     location,
		ThirdPartyRequirements: requirements,
	}, nil
}

// NewPublicKeyDischarge returns a new discharge for the provided 'tp'
// after validating any restrictions specified in it under context 'ctx'.
//
// The ID of the discharge is the same as the ID of 'caveat', and the discharge
// will be usable for the provided 'duration' only if:
// (1) 'caveats' are met when using the discharge.
// (2) 'tp' was obtained from NewPublicKeyCaveat using a key that is the same as
// the provided signer's PublicKey.
func NewPublicKeyDischarge(signer security.PrivateID, tp security.ThirdPartyCaveat, ctx security.Context, duration time.Duration, caveats []security.Caveat) (security.Discharge, error) {
	pkCaveat, ok := tp.(*publicKeyCaveat)
	if !ok {
		return nil, fmt.Errorf("cannot mint discharges for %T", tp)
	}

	v, err := vsecurity.CaveatValidators(pkCaveat.DischargeMintingCaveat)
	if err != nil {
		return nil, err
	}
	if err := v[0].Validate(ctx); err != nil {
		return nil, err
	}

	expiryCaveat, err := security.ExpiryCaveat(time.Now().Add(duration))
	if err != nil {
		return nil, err
	}

	caveats = append(caveats, expiryCaveat)
	discharge := &publicKeyDischarge{CaveatID: tp.ID(), Caveats: caveats}

	// TODO(ashankar,ataly): Should signer necessarily be the same as ctx.LocalID()?
	// If so, need the PrivateID object corresponding to ctx.LocalID.
	if err := discharge.sign(signer); err != nil {
		return nil, err
	}
	return discharge, nil
}

// id calculates the sha256 hash of length-delimited byte slices
func id(slices ...[]byte) string {
	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)

	for _, slice := range slices {
		wire.WriteBytes(h, tmp, slice)
	}
	return string(h.Sum(nil))
}

func init() {
	vom.Register(publicKeyCaveat{})
	vom.Register(publicKeyDischarge{})
}
