// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package principal

import (
	"encoding/base64"

	"v.io/v23/security"
)

func ConvertBlessingsToHandle(blessings security.Blessings, handle BlessingsHandle) *JsBlessings {
	encoded, err := EncodePublicKey(blessings.PublicKey())
	if err != nil {
		panic(err)
	}
	return &JsBlessings{
		Handle:    handle,
		PublicKey: encoded,
	}
}

func EncodePublicKey(key security.PublicKey) (string, error) {
	bytes, err := key.MarshalBinary()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

func DecodePublicKey(key string) (security.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}
	return security.UnmarshalPublicKey(b)
}
