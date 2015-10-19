// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/nacl/secretbox"

	"v.io/v23/security"
)

var (
	// errNoPermission is the error returned by decrypt when there is no permission
	// to decrypt an advertisement.
	errNoPermission = errors.New("no permission")
)

// encrypt identity-based encrypts the service so that only users who match with one of
// the given blessing patterns can decrypt it. Nil patterns means no encryption.
func encrypt(ad *Advertisement, patterns []security.BlessingPattern) error {
	if len(patterns) == 0 {
		ad.EncryptionAlgorithm = NoEncryption
		return nil
	}

	sharedKey, keys, err := newSharedKey(patterns)
	if err != nil {
		return err
	}
	ad.EncryptionAlgorithm = TestEncryption
	ad.EncryptionKeys = keys

	// We only encrypt addresses for now.
	//
	// TODO(jhahn): Revisit the scope of encryption.
	encrypted := make([]string, len(ad.Service.Addrs))
	for i, addr := range ad.Service.Addrs {
		var n [24]byte
		binary.LittleEndian.PutUint64(n[:], uint64(i))
		encrypted[i] = string(secretbox.Seal(nil, []byte(addr), &n, sharedKey))
	}
	ad.Service.Addrs = encrypted
	return nil
}

// decrypt decrypts the service with the given blessing names.
func decrypt(ad *Advertisement, names []string) error {
	if ad.EncryptionAlgorithm == NoEncryption {
		// Not encrypted.
		return nil
	}
	if len(names) == 0 {
		// No identifiers.
		return errNoPermission
	}

	if ad.EncryptionAlgorithm != TestEncryption {
		return fmt.Errorf("unsupported encryption algorithm: %v", ad.EncryptionAlgorithm)
	}
	sharedKey, err := decryptSharedKey(ad.EncryptionKeys, names)
	if err != nil {
		return err
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

// newSharedKey creates a new shared encryption key and identity-based encrypts
// the shared key with the given blessing patterns.
func newSharedKey(patterns []security.BlessingPattern) (*[32]byte, []EncryptionKey, error) {
	var sharedKey [32]byte
	if _, err := rand.Read(sharedKey[:]); err != nil {
		return nil, nil, err
	}

	keys := make([]EncryptionKey, len(patterns))
	// TODO(jhahn): Replace this fake with the real IBE.
	for i, pattern := range patterns {
		var k [32]byte
		copy(k[:], pattern)
		keys[i] = secretbox.Seal(nil, sharedKey[:], &[24]byte{}, &k)
	}
	return &sharedKey, keys, nil
}

// decryptSharedKey decrypts the identity-based encrypted shared key with the
// given blessing names.
func decryptSharedKey(keys []EncryptionKey, names []string) (*[32]byte, error) {
	// TODO(jhahn): Replace this fake with the real IBE.
	for _, name := range names {
		for _, pattern := range prefixPatterns(name) {
			var k [32]byte
			copy(k[:], pattern)
			for _, key := range keys {
				decrypted, ok := secretbox.Open(nil, key, &[24]byte{}, &k)
				if !ok {
					continue
				}
				if len(decrypted) != 32 {
					return nil, errors.New("shared key decryption error")
				}
				var sharedKey [32]byte
				copy(sharedKey[:], decrypted)
				return &sharedKey, nil
			}
		}
	}
	return nil, nil
}

// prefixPatterns returns blessing patterns that can be matched by the given name.
func prefixPatterns(name string) []string {
	patterns := []string{
		name,
		name + security.ChainSeparator + string(security.NoExtension),
	}
	for {
		i := strings.LastIndex(name, security.ChainSeparator)
		if i < 0 {
			break
		}
		name = name[:i]
		patterns = append(patterns, name)
	}
	return patterns
}
