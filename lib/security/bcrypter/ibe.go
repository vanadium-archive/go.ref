// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bcrypter

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"v.io/v23/context"
	"v.io/v23/security"

	"v.io/x/lib/ibe"
)

const (
	// IBE is an identity-based encryption (IBE) scheme with blessing patterns
	// used as identities.
	//
	// Encrypting a message under a blessing pattern involves using an IBE
	// scheme with the pattern as the identity. A blessing is associated with
	// set of identity private keys -- one for each pattern matched by the blessing.
	// Decrypting a message involves choosing the appropriate private key
	// associated with a blessing matching the pattern used in the ciphertext.
	//
	// For space-efficiency, the ciphertext only mentions a truncated hash of the
	// pattern. This scheme uses 16-bytes as the truncation length for the hash.
	IBE            Scheme = 1
	hashTruncation        = 16
)

// ibeEncrypter implements an Encrypter.
type ibeEncrypter struct {
	params ibe.Params
}

func (b *ibeEncrypter) Encrypt(ctx *context.T, forPatterns []security.BlessingPattern, plaintext *Plaintext) (*Ciphertext, error) {
	ciphertext := &Ciphertext{
		Scheme:      IBE,
		Ciphertexts: make(map[string]*ibe.Ciphertext),
	}
	if len(forPatterns) == 0 {
		return ciphertext, nil
	}
	for _, p := range forPatterns {
		var ctxt ibe.Ciphertext
		if err := b.params.Encrypt(string(p), (*ibe.Plaintext)(plaintext), &ctxt); err != nil {
			return nil, NewErrInternal(ctx, err)
		}
		h := hash(p)
		// Verify that the hash does not collide with the hashes of the patterns
		// seen so far in this loop.
		if _, ok := ciphertext.Ciphertexts[h]; ok {
			return nil, NewErrInternal(ctx, fmt.Errorf("cannot encrypt as the hash of the pattern %v collides with one of the other patterns", p))
		}
		ciphertext.Ciphertexts[hash(p)] = &ctxt
	}
	return ciphertext, nil
}

// ibeDecrypter implements a Decrypter.
type ibeDecrypter struct {
	keys map[string]ibe.PrivateKey
}

func (b *ibeDecrypter) Decrypt(ctx *context.T, ciphertext *Ciphertext) (*Plaintext, error) {
	if ciphertext.Scheme != IBE {
		return nil, NewErrInvalidScheme(ctx, int32(ciphertext.Scheme), []int32{int32(IBE)})
	}
	var (
		key       ibe.PrivateKey
		keyFound  bool
		err       error
		plaintext Plaintext
	)
	for p, ctxt := range ciphertext.Ciphertexts {
		key, keyFound = b.keys[p]
		if !keyFound {
			continue
		}
		if err = key.Decrypt(ctxt, (*ibe.Plaintext)(&plaintext)); err == nil {
			break
		}
	}
	if !keyFound {
		return nil, NewErrPrivateKeyNotFound(ctx)
	}
	if err != nil {
		return nil, NewErrInternal(ctx, err)
	}
	return &plaintext, nil
}

// NewIBEEncrypter constucts a new Encrypter using the provided ibe.Params.
func NewIBEEncrypter(params ibe.Params) Encrypter {
	return &ibeEncrypter{params: params}
}

// NewIBEDecrypter constructs a new Decrypter for the provided blessing using
// provided slice of IBE private keys corresponding to the blessing.
// See Also: ExtractPrivateKeys.
func NewIBEDecrypter(blessing string, privateKeys []ibe.PrivateKey) (Decrypter, error) {
	if len(blessing) == 0 {
		return nil, errors.New("blessing cannot be empty")
	}
	decrypter := &ibeDecrypter{keys: make(map[string]ibe.PrivateKey)}
	patterns := matchedBy(blessing)
	if got, want := len(privateKeys), len(patterns); got != want {
		return nil, fmt.Errorf("got %d private keys for blessing %v, expected %d", got, blessing, want)
	}
	for i, p := range patterns {
		decrypter.keys[hash(p)] = privateKeys[i]
	}
	return decrypter, nil
}

// ExtractPrivateKeys returns a slice of IBE private keys for the provided
// blessing, extracted using the provided IBE Master.
//
// The slice of private keys contains private keys extracted for each blessing
// pattern matched by the blessing (i.e., the blessing pattern string is
// the identity for which the private key is extracted). Furthermore, the private
// keys are organized in increasing order of the lengths of the corresponding
// patterns.
func ExtractPrivateKeys(master ibe.Master, blessing string) ([]ibe.PrivateKey, error) {
	if len(blessing) == 0 {
		return nil, errors.New("blessing must be non-empty")
	}
	patterns := matchedBy(blessing)
	keys := make([]ibe.PrivateKey, len(patterns))
	for i, p := range patterns {
		ibeKey, err := master.Extract(string(p))
		if err != nil {
			return nil, err
		}
		keys[i] = ibeKey
	}
	return keys, nil
}

// matchedBy returns the set of blessing patterns (in increasing order
// of length) matched by the provided blessing. The provided blessing
// must be non-empty.
func matchedBy(blessing string) []security.BlessingPattern {
	patterns := make([]security.BlessingPattern, strings.Count(blessing, security.ChainSeparator)+2)
	patterns[len(patterns)-1] = security.BlessingPattern(blessing) + security.ChainSeparator + security.NoExtension
	patterns[len(patterns)-2] = security.BlessingPattern(blessing)
	for idx := len(patterns) - 3; idx >= 0; idx-- {
		blessing = blessing[0:strings.LastIndex(blessing, string(security.ChainSeparator))]
		patterns[idx] = security.BlessingPattern(blessing)
	}
	return patterns
}

// hash returns a 128-bit truncated SHA-256 hash of a blessing pattern.
func hash(pattern security.BlessingPattern) string {
	h := sha256.Sum256([]byte(pattern))
	truncated := h[:hashTruncation]
	return string(truncated)
}
