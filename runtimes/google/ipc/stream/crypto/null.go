package crypto

import "v.io/veyron/veyron/runtimes/google/lib/iobuf"

// NewNullCrypter returns a Crypter that does no encryption/decryption.
func NewNullCrypter() Crypter { return null{} }

type null struct{}

func (null) Encrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (null) Decrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (null) String() string                                 { return "Null" }
func (null) ChannelBinding() []byte                         { return nil }
