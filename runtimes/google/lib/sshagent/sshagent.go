// Package sshagent implements a type to use ssh-agent to store ECDSA private keys and sign slices using them.
//
// For unix-based systems, the implementation is based on the OpenSSH implementation of ssh-agent, with the protocol defined in
// revision 1.7 of the PROTOCOL.agent file (http://www.openbsd.org/cgi-bin/cvsweb/src/usr.bin/ssh/PROTOCOL.agent?rev=1.7;content-type=text%2Fplain)
package sshagent

import (
	"crypto/ecdsa"
	"math/big"
)

// Agent is the interface for communicating with an ssh-agent process.
type Agent interface {
	// List returns the set of public keys and comments associated with them
	// stored in the SSH agent.
	List() (keys []*ecdsa.PublicKey, comments []string, err error)
	// Add adds a (private key, comment) pair to the SSH agent.
	Add(priv *ecdsa.PrivateKey, comment string) error
	// Remove removes the private key associated with the provided public key
	// from the SSH agent.
	Remove(pub *ecdsa.PublicKey) error
	// Sign signs the SHA-256 hash of data using the private key stored in the
	// SSH agent correponding to pub.
	Sign(pub *ecdsa.PublicKey, data []byte) (R, S *big.Int, err error)
}
