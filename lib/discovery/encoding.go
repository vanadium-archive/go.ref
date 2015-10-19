// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// PackAddresses packs addresses into a byte slice.
func PackAddresses(addrs []string) []byte {
	var buf bytes.Buffer
	for _, a := range addrs {
		writeInt(&buf, len(a))
		buf.WriteString(a)
	}
	return buf.Bytes()
}

// UnpackAddresses unpacks addresses from a byte slice.
func UnpackAddresses(data []byte) ([]string, error) {
	var addrs []string
	for r := bytes.NewBuffer(data); r.Len() > 0; {
		n, err := readInt(r)
		if err != nil {
			return nil, err
		}
		b := r.Next(n)
		if len(b) != n {
			return nil, errors.New("invalid addresses")
		}
		addrs = append(addrs, string(b))
	}
	return addrs, nil
}

// PackEncryptionKeys packs encryption algorithm and keys into a byte slice.
func PackEncryptionKeys(algo EncryptionAlgorithm, keys []EncryptionKey) []byte {
	var buf bytes.Buffer
	writeInt(&buf, int(algo))
	for _, k := range keys {
		writeInt(&buf, len(k))
		buf.Write(k)
	}
	return buf.Bytes()
}

// UnpackEncryptionKeys unpacks encryption algorithm and keys from a byte slice.
func UnpackEncryptionKeys(data []byte) (EncryptionAlgorithm, []EncryptionKey, error) {
	buf := bytes.NewBuffer(data)
	algo, err := readInt(buf)
	if err != nil {
		return NoEncryption, nil, err
	}
	var keys []EncryptionKey
	for buf.Len() > 0 {
		n, err := readInt(buf)
		if err != nil {
			return NoEncryption, nil, err
		}
		v := buf.Next(n)
		if len(v) != n {
			return NoEncryption, nil, errors.New("invalid encryption keys")
		}
		keys = append(keys, EncryptionKey(v))
	}
	return EncryptionAlgorithm(algo), keys, nil
}

func writeInt(w io.Writer, x int) {
	var b [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(b[:], uint64(x))
	w.Write(b[0:n])
}

func readInt(r io.ByteReader) (int, error) {
	x, err := binary.ReadUvarint(r)
	return int(x), err
}
