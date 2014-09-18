package security

import "veyron.io/veyron/veyron2/security"

// NewPrivateID returns a new PrivateID that uses the provided Signer to generate
// signatures.  The returned PrivateID additionaly contains a single self-signed
// certificate with the given name.
//
// If a nil signer is provided, this method will generate a new public/private key pair
// and use a system-default signer, which stores the private key in the clear in the memory
// of the running process.
func NewPrivateID(name string, signer security.Signer) (security.PrivateID, error) {
	return newChainPrivateID(name, signer)
}
