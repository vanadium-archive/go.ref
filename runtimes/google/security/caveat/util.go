package caveat

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"time"

	"veyron/runtimes/google/security/wire"
	"veyron/security/caveat"

	"veyron2/security"
)

// NewPublicKeyCaveat returns a new third-party caveat from the provided restriction,
// third-party identity, and third-party location.
func NewPublicKeyCaveat(restriction string, thirdParty security.PublicID, location string) (security.ThirdPartyCaveat, error) {
	nonce := make([]uint8, nonceLength)
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	var validationKey wire.PublicKey
	if err := validationKey.Encode(thirdParty.PublicKey()); err != nil {
		return nil, err
	}

	return &PublicKeyCaveat{
		RandNonce:          nonce,
		Restriction:        restriction,
		ValidationKey:      validationKey,
		ThirdPartyLocation: location,
	}, nil
}

// NewPublicKeyDischarge returns a new discharge for the provided PublicKeyCaveat.
// The CaveatID of the discharge is the same as the ID of the caveat, and
// the discharge includes the provided service caveats along with a universal
// expiry caveat for the provided duration. The discharge also includes a
// signature over its contents obtained from the provided private key.
func NewPublicKeyDischarge(cav *PublicKeyCaveat, dischargingKey *ecdsa.PrivateKey, duration time.Duration, caveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	discharge := &PublicKeyDischarge{
		RandNonce:   cav.RandNonce,
		Restriction: cav.Restriction,
	}

	now := time.Now()
	expiryCaveat := &caveat.Expiry{IssueTime: now, ExpiryTime: now.Add(duration)}
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

// sign uses the provided private key to sign the contents of the discharge. The private
// key typically belongs to the principal that minted the discharge.
func (d *PublicKeyDischarge) sign(key *ecdsa.PrivateKey) error {
	r, s, err := ecdsa.Sign(rand.Reader, key, d.contentHash())
	if err != nil {
		return err
	}
	d.Signature.R = r.Bytes()
	d.Signature.S = s.Bytes()
	return nil
}

func (d *PublicKeyDischarge) contentHash() []byte {
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
