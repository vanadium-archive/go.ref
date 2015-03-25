// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import "v.io/x/ref/profiles/internal/lib/iobuf"

// NewNullCrypter returns a Crypter that does no encryption/decryption.
func NewNullCrypter() Crypter { return null{} }

type null struct{}

func (null) Encrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (null) Decrypt(src *iobuf.Slice) (*iobuf.Slice, error) { return src, nil }
func (null) String() string                                 { return "Null" }
func (null) ChannelBinding() []byte                         { return nil }
