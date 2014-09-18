// Package crypto implements encryption and decryption interfaces intended for
// securing communication over VCs.
package crypto

import "veyron.io/veyron/veyron/runtimes/google/lib/iobuf"

type Encrypter interface {
	// Encrypt encrypts the provided plaintext data and returns the
	// corresponding ciphertext slice (or nil if an error is returned).
	//
	// It always calls Release on plaintext and thus plaintext should not
	// be used after calling Encrypt.
	Encrypt(plaintext *iobuf.Slice) (ciphertext *iobuf.Slice, err error)
}

type Decrypter interface {
	// Decrypt decrypts the provided ciphertext slice and returns the
	// corresponding plaintext (or nil if an error is returned).
	//
	// It always calls Release on ciphertext and thus ciphertext should not
	// be used after calling Decrypt.
	Decrypt(ciphertext *iobuf.Slice) (plaintext *iobuf.Slice, err error)
}

type Crypter interface {
	Encrypter
	Decrypter
	// ChannelBinding Returns a byte slice that is unique for the the
	// particular crypter (and the parties between which it is operationg).
	// Having both parties assert out of the band that they are indeed
	// participating in a connection with that channel binding value is
	// sufficient to authenticate the data received through the crypter.
	ChannelBinding() []byte
	String() string
}
