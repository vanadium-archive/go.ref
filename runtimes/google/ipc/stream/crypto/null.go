package crypto

import "veyron/runtimes/google/lib/iobuf"

// NewNullCrypter returns a Crypter that does no encryption/decryption.
func NewNullCrypter() Crypter { return null{} }

type null struct{}

func (c null) Encrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (c null) Decrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (c null) String() string                                 { return "Null" }
