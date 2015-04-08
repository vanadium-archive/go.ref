// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server_test

import (
	"math/big"
	"testing"

	"v.io/v23/security"
	"v.io/x/ref/services/identity/internal/server"
	"v.io/x/ref/services/identity/internal/signer/v1"
)

func TestDecode(t *testing.T) {
	encodedKey := &signer.PublicKey{Base64: "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEPqDbuT2B9Bb3JMcOGd2mm4bQuKSREeSKRt8_oofo0jRYiKFQ2ZVuCqssA-IUvFArT5KfXc6B9BNesgS10rPKrg=="}
	encodedSig := &signer.VSignature{R: "0x42bca58e435f906c874536789cfc31656dd8f9ffbd3b7be84181611cc04eaf74", S: "0xa6f57e858a9f36b559e9cd9f13854b90fad49e0c5591ed66033fd286682b2078"}

	key, err := server.DecodePublicKey(encodedKey)
	if err != nil {
		t.Fatal(err)
	}

	s := security.NewECDSASigner(key, func(message []byte) (r, s *big.Int, err error) {
		return server.DecodeSignature(encodedSig)
	})
	sig, err := s.Sign([]byte("purpose"), []byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	if !sig.Verify(s.PublicKey(), []byte("message")) {
		t.Fatal("Signature does not verify")
	}
}
