// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"encoding/binary"
	"errors"

	"v.io/x/ref/lib/security/bcrypter"
)

// PackAddresses packs addresses into a byte slice.
func PackAddresses(addrs []string) []byte {
	buf := newBuffer(nil)
	for _, a := range addrs {
		buf.writeString(a)
	}
	return buf.Bytes()
}

// UnpackAddresses unpacks addresses from a byte slice.
func UnpackAddresses(data []byte) ([]string, error) {
	buf := newBuffer(data)
	var addrs []string
	for buf.Len() > 0 {
		addr, err := buf.readString()
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// PackEncryptionKeys packs encryption algorithm and keys into a byte slice.
func PackEncryptionKeys(algo EncryptionAlgorithm, keys []EncryptionKey) []byte {
	buf := newBuffer(nil)
	buf.writeInt(int(algo))
	for _, k := range keys {
		buf.writeBytes(k)
	}
	return buf.Bytes()
}

// UnpackEncryptionKeys unpacks encryption algorithm and keys from a byte slice.
func UnpackEncryptionKeys(data []byte) (EncryptionAlgorithm, []EncryptionKey, error) {
	buf := newBuffer(data)
	algo, err := buf.readInt()
	if err != nil {
		return NoEncryption, nil, err
	}
	var keys []EncryptionKey
	for buf.Len() > 0 {
		key, err := buf.readBytes()
		if err != nil {
			return NoEncryption, nil, err
		}
		keys = append(keys, EncryptionKey(key))
	}
	return EncryptionAlgorithm(algo), keys, nil
}

// EncodeCiphertext encodes the cipher text into a byte slice.
func EncodeWireCiphertext(wctext *bcrypter.WireCiphertext) []byte {
	buf := newBuffer(nil)
	buf.writeString(wctext.PatternId)
	for k, v := range wctext.Bytes {
		buf.writeString(k)
		buf.writeBytes(v)
	}
	return buf.Bytes()
}

// DecodeCiphertext decodes the cipher text from a byte slice.
func DecodeWireCiphertext(data []byte) (*bcrypter.WireCiphertext, error) {
	buf := newBuffer(data)
	id, err := buf.readString()
	if err != nil {
		return nil, err
	}
	wctext := bcrypter.WireCiphertext{id, make(map[string][]byte)}
	for buf.Len() > 0 {
		k, err := buf.readString()
		if err != nil {
			return nil, err
		}
		v, err := buf.readBytes()
		if err != nil {
			return nil, err
		}
		wctext.Bytes[k] = v
	}
	return &wctext, nil
}

type buffer struct {
	*bytes.Buffer
}

func (b *buffer) writeInt(x int) {
	var p [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(p[:], uint64(x))
	b.Write(p[0:n])
}

func (b *buffer) readInt() (int, error) {
	x, err := binary.ReadUvarint(b)
	return int(x), err
}

func (b *buffer) writeBytes(p []byte) {
	b.writeInt(len(p))
	b.Write(p)
}

func (b *buffer) readBytes() ([]byte, error) {
	n, err := b.readInt()
	if err != nil {
		return nil, err
	}
	p := b.Next(n)
	if len(p) != n {
		return nil, errors.New("odd length data")
	}
	return p, nil
}

func (b *buffer) writeString(s string) {
	b.writeInt(len(s))
	b.WriteString(s)
}

func (b *buffer) readString() (string, error) {
	p, err := b.readBytes()
	if err != nil {
		return "", err
	}
	return string(p), nil
}

func newBuffer(data []byte) *buffer { return &buffer{bytes.NewBuffer(data)} }
