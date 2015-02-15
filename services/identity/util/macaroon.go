package util

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

// Macaroon encapsulates an arbitrary slice of data with an HMAC for integrity protection.
// Term borrowed from http://research.google.com/pubs/pub41892.html.
type Macaroon string

// NewMacaroon creates an opaque token that encodes "data".
//
// Input can be extracted from the returned token only if the key provided to NewMacaroon is known.
func NewMacaroon(key, data []byte) Macaroon {
	return Macaroon(b64encode(append(data, computeHMAC(key, data)...)))
}

// Decode returns the input if the macaroon is decodable with the provided key.
func (m Macaroon) Decode(key []byte) (input []byte, err error) {
	decoded, err := b64decode(string(m))
	if err != nil {
		return nil, err
	}
	if len(decoded) < sha256.Size {
		return nil, fmt.Errorf("invalid macaroon, too small")
	}
	data := decoded[:len(decoded)-sha256.Size]
	decodedHMAC := decoded[len(decoded)-sha256.Size:]
	if !bytes.Equal(decodedHMAC, computeHMAC(key, data)) {
		return nil, fmt.Errorf("invalid macaroon, HMAC does not match")
	}
	return data, nil
}

func computeHMAC(key, data []byte) []byte {
	hm := hmac.New(sha256.New, key)
	hm.Write(data)
	return hm.Sum(nil)
}
