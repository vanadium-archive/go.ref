// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/ref/lib/security/bcrypter"
)

func encrypt(ctx *context.T, v interface{}, patterns []security.BlessingPattern) ([]bcrypter.WireCiphertext, error) {
	crypter := bcrypter.GetCrypter(ctx)
	if crypter == nil {
		return nil, NewErrNoCrypter(ctx)
	}
	b, err := vom.Encode(v)
	if err != nil {
		return nil, err
	}
	ciphertexts := make([]bcrypter.WireCiphertext, len(patterns))
	for i, p := range patterns {
		ctxt, err := crypter.Encrypt(ctx, p, b)
		if err != nil {
			return nil, err
		}
		ctxt.ToWire(&ciphertexts[i])
	}
	return ciphertexts, nil
}

func decrypt(ctx *context.T, ciphertexts []bcrypter.WireCiphertext, v interface{}) error {
	crypter := bcrypter.GetCrypter(ctx)
	if crypter == nil {
		return NewErrNoCrypter(ctx)
	}
	var ctxt bcrypter.Ciphertext

	for _, c := range ciphertexts {
		ctxt.FromWire(c)
		if data, err := crypter.Decrypt(ctx, &ctxt); err != nil {
			continue
		} else if err := vom.Decode(data, v); err != nil {
			// Since we use a CCA-2 secure IBE scheme, the ciphertext
			// is not malleable. Therefore if decryption succeeds it
			// ought to be that this crypter has the appropriate private
			// key. Any errors in vom decoding the decrypted plaintext
			// are system errors and must be returned.
			return err
		} else {
			return nil
		}
	}
	return NewErrNoPrivateKey(ctx)
}
