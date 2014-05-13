package security

import "veyron2/security"

// NewPrivateID returns a new PrivateID containing a freshly generated
// private key, and a single self-signed certificate specifying the provided
// name and the public key corresponding to the generated private key.
func NewPrivateID(name string) (security.PrivateID, error) {
	return newChainPrivateID(name)
}
