// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bcrypter

import (
	"bytes"
	"testing"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/lib/ibe"
)

func TestIBECrypter(t *testing.T) {
	blessing := "google/bob/tablet"
	newPlaintext := func() [32]byte {
		var m [32]byte
		if n := copy(m[:], []byte("AThirtyTwoBytePieceOfTextThisIs!")); n != len(m) {
			t.Fatalf("plaintext string must be %d bytes, not %d", len(m), n)
		}
		return m
	}
	master, err := ibe.SetupBB1()
	if err != nil {
		t.Fatal(err)
	}
	privateKeys, err := ExtractPrivateKeys(master, blessing)
	if err != nil {
		t.Fatal(err)
	}

	encrypter := NewIBEEncrypter(master.Params())
	decrypter, err := NewIBEDecrypter(blessing, privateKeys)
	if err != nil {
		t.Fatal(err)
	}
	msg := newPlaintext()

	ctx, shutdown := context.RootContext()
	defer shutdown()

	// Validate that bob's tablets can only decrypt messages encrypted
	// for patterns matched by its blessings.
	test := struct {
		valid, invalid [][]security.BlessingPattern
	}{
		valid: [][]security.BlessingPattern{
			[]security.BlessingPattern{"google"},
			[]security.BlessingPattern{"google/bob"},
			[]security.BlessingPattern{"google/bob/tablet"},
			[]security.BlessingPattern{"google/bob/tablet/$"},
			[]security.BlessingPattern{"google/bob", "google/$"},
		},
		invalid: [][]security.BlessingPattern{
			nil,
			[]security.BlessingPattern{"google/$"},
			[]security.BlessingPattern{"google/bob/$", "samsung/tablet"},
			[]security.BlessingPattern{"google/bob/tablet/youtube"},
		},
	}
	var (
		ciphertext *security.Ciphertext
		plaintext  *[32]byte
	)
	for _, patterns := range test.valid {
		if ciphertext, err = encrypter.Encrypt(ctx, patterns, &msg); err != nil {
			t.Fatal(err)
		}
		if plaintext, err = decrypter.Decrypt(ctx, ciphertext); err != nil || !bytes.Equal((*plaintext)[:], msg[:]) {
			t.Fatalf("Ciphertext for patterns %v: decryption returned %v, want nil", patterns, err)
		}
	}
	for _, patterns := range test.invalid {
		if ciphertext, err = encrypter.Encrypt(ctx, patterns, &msg); err != nil {
			t.Fatal(err)
		}
		if plaintext, err = decrypter.Decrypt(ctx, ciphertext); verror.ErrorID(err) != ErrPrivateKeyNotFound.ID {
			t.Fatalf("Ciphertext for patterns %v, decryption succeeded, wanted it to fail", patterns)
		}
	}
}
