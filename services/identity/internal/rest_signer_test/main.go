// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"fmt"

	"v.io/x/ref/services/identity/internal/server"
)

func main() {
	signer, err := server.NewRestSigner()
	if err != nil {
		fmt.Printf("NewRestSigner error: %v\n", err)
		return
	}
	sig, err := signer.Sign([]byte("purpose"), []byte("message"))
	if err != nil {
		fmt.Printf("Sign error: %v\n", err)
		return
	}
	ok := sig.Verify(signer.PublicKey(), []byte("message"))
	fmt.Printf("Verified: %v\n", ok)
}
