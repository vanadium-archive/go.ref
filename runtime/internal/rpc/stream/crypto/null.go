// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import "v.io/x/ref/runtime/internal/lib/iobuf"

// NewNullCrypter returns a Crypter that does no encryption/decryption.
func NewNullCrypter() Crypter { return null{} }

// NewNullCrypterWithChannelBinding returns a null Crypter with a channel binding.
func NewNullCrypterWithChannelBinding(channelBinding []byte) Crypter {
	return null{channelBinding}
}

type null struct {
	channelBinding []byte
}

func (null) Encrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (null) Decrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (n null) ChannelBinding() []byte                       { return n.channelBinding }
func (n null) String() string {
	if n.channelBinding == nil {
		return "Null"
	}
	return "Null(ChannelBinding)"
}
