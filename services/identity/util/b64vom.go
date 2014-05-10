package util

import (
	"bytes"
	"encoding/base64"

	"veyron2/vom"
)

// Bas64VomEncode returns the base64 encoding of the serialization of i with
// vom.
func Base64VomEncode(i interface{}) (string, error) {
	buf := &bytes.Buffer{}
	closer := base64.NewEncoder(base64.URLEncoding, buf)
	if err := vom.NewEncoder(closer).Encode(i); err != nil {
		return "", err
	}
	// Must close the base64 encoder to flush out any partially written
	// blocks.
	if err := closer.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Base64VomDecode is the reverse of encode - filling in i after vom-decoding
// the base64-encoded string s.
func Base64VomDecode(s string, i interface{}) error {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return vom.NewDecoder(bytes.NewBuffer(b)).Decode(i)
}
