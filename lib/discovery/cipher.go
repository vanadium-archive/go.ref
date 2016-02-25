// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/secretbox"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/vom"

	"v.io/x/ref/lib/security/bcrypter"
)

var (
	// errNoPermission is the error returned by decrypt when there is no permission
	// to decrypt an advertisement.
	errNoPermission = errors.New("no permission")
)

// encrypt encrypts the service so that only users who possess blessings
// matching one of the given blessing patterns can decrypt it. Nil patterns
// means no encryption.
func encrypt(ctx *context.T, ad *Advertisement, patterns []security.BlessingPattern) error {
	if len(patterns) == 0 {
		ad.EncryptionAlgorithm = NoEncryption
		return nil
	}

	var sharedKey [32]byte
	if _, err := rand.Read(sharedKey[:]); err != nil {
		return err
	}

	ad.EncryptionAlgorithm = IbeEncryption
	ad.EncryptionKeys = make([]EncryptionKey, len(patterns))
	var err error
	for i, pattern := range patterns {
		if ad.EncryptionKeys[i], err = wrapSharedKey(ctx, sharedKey, pattern); err != nil {
			return err
		}
	}

	// We only encrypt addresses for now.
	//
	// TODO(jhahn): Revisit the scope of encryption.
	encrypted := make([]string, len(ad.Service.Addrs))
	for i, addr := range ad.Service.Addrs {
		var n [24]byte
		binary.LittleEndian.PutUint64(n[:], uint64(i))
		encrypted[i] = string(secretbox.Seal(nil, []byte(addr), &n, &sharedKey))
	}
	ad.Service.Addrs = encrypted
	return nil
}

// decrypt decrypts the service using a blessings-based crypter
// from the provided context.
//
// TODO(ataly, ashankar, jhahn): Currently we are using the go
// implementation of the 'bn256' pairings library which causes
// the IBE decryption cost to be 42ms. As a result, clients
// processing discovery advertisements ust use conservative timeouts
// of at least 200ms to ensure that the advertisement is decrypted.
// Once we switch to the C implementation of the library, the
// decryption cost, and therefore this timeout, will come down.
func decrypt(ctx *context.T, ad *Advertisement) error {
	if ad.EncryptionAlgorithm == NoEncryption {
		// Not encrypted.
		return nil
	}

	if ad.EncryptionAlgorithm != IbeEncryption {
		return fmt.Errorf("unsupported encryption algorithm: %v", ad.EncryptionAlgorithm)
	}

	var (
		sharedKey *[32]byte
		err       error
	)
	for _, key := range ad.EncryptionKeys {
		if sharedKey, err = unwrapSharedKey(ctx, key); err == nil {
			break
		}
	}
	if sharedKey == nil {
		return errNoPermission
	}

	// We only encrypt addresses for now.
	//
	// Note that we should not modify the slice element directly here since the
	// underlying plugins may cache services and the next plugin.Scan() may return
	// the already decrypted addresses.
	decrypted := make([]string, len(ad.Service.Addrs))
	for i, encrypted := range ad.Service.Addrs {
		var n [24]byte
		binary.LittleEndian.PutUint64(n[:], uint64(i))
		addr, ok := secretbox.Open(nil, []byte(encrypted), &n, sharedKey)
		if !ok {
			return errors.New("decryption error")
		}
		decrypted[i] = string(addr)
	}
	ad.Service.Addrs = decrypted
	return nil
}

func wrapSharedKey(ctx *context.T, sharedKey [32]byte, pattern security.BlessingPattern) (EncryptionKey, error) {
	crypter := bcrypter.GetCrypter(ctx)
	ctxt, err := crypter.Encrypt(ctx, pattern, sharedKey[:])
	if err != nil {
		return nil, err
	}
	return encodeEncryptionKey(ctxt)
}

func unwrapSharedKey(ctx *context.T, key EncryptionKey) (*[32]byte, error) {
	crypter := bcrypter.GetCrypter(ctx)
	ctxt, err := decodeEncryptionKey(key)
	if err != nil {
		return nil, err
	}
	decrypted, err := crypter.Decrypt(ctx, ctxt)
	if err != nil {
		return nil, err
	}
	if decryptedLen := len(decrypted); decryptedLen != 32 {
		return nil, fmt.Errorf("decrypted shared key has length %v, want %v", decryptedLen, 32)
	}
	var sharedKey [32]byte
	copy(sharedKey[:], decrypted)
	return &sharedKey, nil
}

func encodeEncryptionKey(ctxt *bcrypter.Ciphertext) (EncryptionKey, error) {
	var wctxt bcrypter.WireCiphertext
	ctxt.ToWire(&wctxt)
	// TODO(ataly, jhahn): Use a more efficient encoding than
	// VOM, if possible.
	b, err := vom.Encode(wctxt)
	if err != nil {
		return nil, err
	}
	return EncryptionKey(b), nil
}

func decodeEncryptionKey(key EncryptionKey) (*bcrypter.Ciphertext, error) {
	var (
		wctxt bcrypter.WireCiphertext
		ctxt  bcrypter.Ciphertext
	)
	if err := vom.Decode([]byte(key), &wctxt); err != nil {
		return nil, err
	}
	ctxt.FromWire(wctxt)
	return &ctxt, nil
}
