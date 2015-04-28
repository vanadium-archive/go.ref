// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"v.io/v23/vom"
)

func VomEncode(v interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := vom.NewEncoder(&buf)
	if err := encoder.Encode(v); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf.Bytes()), nil
}

func VomEncodeOrDie(v interface{}) string {
	s, err := VomEncode(v)
	if err != nil {
		panic(err)
	}
	return s
}

func VomDecode(data string, v interface{}) error {
	binbytes, err := hex.DecodeString(data)
	if err != nil {
		return fmt.Errorf("Error decoding hex string %q: %v", data, err)
	}
	decoder := vom.NewDecoder(bytes.NewReader(binbytes))
	return decoder.Decode(v)
}
