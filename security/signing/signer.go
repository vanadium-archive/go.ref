package signing

import (
	"crypto/ecdsa"
	"crypto/rand"

	"veyron2/security"
)

// NewClearSigner creates a Signer that uses the provided private key to sign messages.
// This private key is kept in the clear in the memory of the running process.
// No hashing is applied prior to signing.
func NewClearSigner(key *ecdsa.PrivateKey) security.Signer {
	return &clearSigner{key, security.NewECDSAPublicKey(&key.PublicKey)}
}

type clearSigner struct {
	key    *ecdsa.PrivateKey
	pubkey security.PublicKey
}

func (c *clearSigner) Sign(message []byte) (sig security.Signature, err error) {
	r, s, err := ecdsa.Sign(rand.Reader, c.key, message)
	if err != nil {
		return
	}
	sig.R, sig.S = r.Bytes(), s.Bytes()
	sig.Hash = security.NoHash
	return
}

func (c *clearSigner) PublicKey() security.PublicKey {
	return c.pubkey
}
