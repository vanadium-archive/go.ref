package caveat

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

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

// errPublicKeyCaveat returns an error indicating that the provided caveat has
// an invalid or missing discharge.
func errPublicKeyCaveat(c *publicKeyCaveat, err error) error {
	return fmt.Errorf("%v for publicKeyCaveat{nonce:%v location:%q}", err, c.RandNonce, c.Location())
}

// publicKeyCaveat implements security.ThirdPartyCaveat. It specifies a restriction,
// a validation key and the location of the third-party responsible for
// discharging the caveat. A discharge for this caveat is a signed assertion whose
// signature can be verified using the validation key.
type publicKeyCaveat struct {
	// RandNonce specifies a cryptographically random nonce (of fixed length) that
	// uniquely identifies the caveat.
	RandNonce []uint8
	// DischargeMintingCaveat specifies the caveat that has to be validated
	// before minting a discharge for a publicKeyCaveat. A byte slice containing
	// VOM-encoded security.Caveat is used to enable a publicKeyCaveat to be
	// validated by devices that cannot decode the discharge minting caveats.
	DischargeMintingCaveat []byte
	// ValidationKey specifies the public key of the discharging-party.
	ValidationKey wire.PublicKey
	// ThirdPartyLocation specifies the object name of the discharging-party.
	ThirdPartyLocation string
	// Information wanted in order to issue a discharge.
	ThirdPartyRequirements security.ThirdPartyRequirements
}

// ID returns a unique 32bytes long identity for the caveat based on the random nonce
// and SHA-256 hash of the restriction embedded in the caveat.
//
// TODO(ataly, ashankar): A 256bit hash is probably much stronger that what we need
// here. Can we truncate the hash to 96bits?
func (c *publicKeyCaveat) ID() security.ThirdPartyCaveatID {
	return security.ThirdPartyCaveatID(id(c.RandNonce, c.DischargeMintingCaveat))
}

// Location returns the third-party location embedded in the caveat.
func (c *publicKeyCaveat) Location() string {
	return c.ThirdPartyLocation
}

func (c *publicKeyCaveat) String() string {
	return fmt.Sprintf("publicKeyCaveat{DischargeMintingCaveat: (%v bytes), ThirdPartyLocation: %q}", len(c.DischargeMintingCaveat), c.ThirdPartyLocation)
}

func (c *publicKeyCaveat) Requirements() security.ThirdPartyRequirements {
	return c.ThirdPartyRequirements
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
	if !pkDischarge.Signature.Verify(key, pkDischarge.contentHash()) {
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
	// ThirdPartyCaveatID is used to match a Discharge with the Caveat it is for.
	ThirdPartyCaveatID security.ThirdPartyCaveatID

	// Caveats under which this Discharge is valid.
	Caveats []wire.Caveat

	// Signature on the contents of the discharge obtained using the private key
	// corresponding to the validaton key in the caveat.
	Signature security.Signature
}

// CaveatID returns a unique identity for the discharge based on the random nonce and
// restriction embedded in the discharge.
func (d *publicKeyDischarge) CaveatID() security.ThirdPartyCaveatID {
	return d.ThirdPartyCaveatID
}

func (d *publicKeyDischarge) ThirdPartyCaveats() []security.ServiceCaveat {
	return wire.DecodeThirdPartyCaveats(d.Caveats)
}

// sign uses the provided identity to sign the contents of the discharge.
func (d *publicKeyDischarge) sign(discharger security.PrivateID) (err error) {
	d.Signature, err = discharger.Sign(d.contentHash())
	return
}

func (d *publicKeyDischarge) contentHash() []byte {
	h := sha256.New()
	tmp := make([]byte, binary.MaxVarintLen64)

	wire.WriteBytes(h, tmp, []byte(d.ThirdPartyCaveatID))
	for _, cav := range d.Caveats {
		wire.WriteString(h, tmp, string(cav.Service))
		wire.WriteBytes(h, tmp, cav.Bytes)
	}
	return h.Sum(nil)
}

// NewPublicKeyCaveat returns a new third-party caveat from the provided restriction,
// third-party identity, and third-party location.
func NewPublicKeyCaveat(dischargeMintingCaveat security.Caveat, thirdParty security.PublicID, location string, requirements security.ThirdPartyRequirements) (security.ThirdPartyCaveat, error) {
	nonce := make([]uint8, nonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	var validationKey wire.PublicKey
	if err := validationKey.Encode(thirdParty.PublicKey()); err != nil {
		return nil, err
	}

	var mintingCaveatEncoded bytes.Buffer
	vom.NewEncoder(&mintingCaveatEncoded).Encode(dischargeMintingCaveat)
	return &publicKeyCaveat{
		RandNonce:              nonce,
		DischargeMintingCaveat: mintingCaveatEncoded.Bytes(),
		ValidationKey:          validationKey,
		ThirdPartyLocation:     location,
		ThirdPartyRequirements: requirements,
	}, nil
}

// NewPublicKeyDischarge returns a new discharge for the provided caveat
// after verifying that the caveats for minting a discharge are met.
//
// The CaveatID of the discharge is the same as the ID of the caveat, and
// the discharge includes the provided service caveats along with a universal
// expiry caveat for the provided duration. The discharge also includes a
// signature over its contents obtained from the provided private key.
func NewPublicKeyDischarge(discharger security.PrivateID, caveat security.ThirdPartyCaveat, ctx security.Context, duration time.Duration, caveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	cav, ok := caveat.(*publicKeyCaveat)
	if !ok {
		return nil, fmt.Errorf("cannot mint discharges for %T", caveat)
	}
	var mintingCaveat security.Caveat
	mcBuf := bytes.NewReader(cav.DischargeMintingCaveat)
	if err := vom.NewDecoder(mcBuf).Decode(&mintingCaveat); err != nil {
		return nil, fmt.Errorf("failed to decode DischargeMintingCaveat: %s", err)
	}
	if err := mintingCaveat.Validate(ctx); err != nil {
		return nil, fmt.Errorf("failed to validate DischargeMintingCaveat: %s", err)
	}
	now := time.Now()
	expiryCaveat := &Expiry{IssueTime: now, ExpiryTime: now.Add(duration)}
	caveats = append(caveats, UniversalCaveat(expiryCaveat))
	encodedCaveats, err := wire.EncodeCaveats(caveats)
	if err != nil {
		return nil, fmt.Errorf("failed to encode caveats in discharge: %v", err)
	}
	discharge := &publicKeyDischarge{
		ThirdPartyCaveatID: caveat.ID(),
		Caveats:            encodedCaveats,
	}
	// TODO(ashankar,ataly): Should discharger necessarily be the same as ctx.LocalID()?
	// If so, need the PrivateID object corresponding to ctx.LocalID.
	if err := discharge.sign(discharger); err != nil {
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
