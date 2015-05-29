// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"v.io/v23/verror"
)

const pkgPath = "v.io/x/ref/services/wspr/internal/lib"

const uint64Size = 8

var (
	errInvalid      = verror.Register(pkgPath+".errInvalid", verror.NoRetry, "{1:}{2:} wspr: invalid encoding{:_}")
	errEOF          = verror.Register(pkgPath+".errEOF", verror.NoRetry, "{1:}{2:} wspr: eof{:_}")
	errUintOverflow = verror.Register(pkgPath+".errUintOverflow", verror.NoRetry, "{1:}{2:} wspr: scalar larger than 8 bytes{:_}")
)

// This code has been copied from the vom package and should be kept up to date
// with it.

// Unsigned integers are the basis for all other primitive values.  This is a
// two-state encoding.  If the number is less than 128 (0 through 0x7f), its
// value is written directly.  Otherwise the value is written in big-endian byte
// order preceded by the negated byte length.
func BinaryEncodeUint(v uint64) []byte {
	switch {
	case v <= 0x7f:
		return []byte{byte(v)}
	case v <= 0xff:
		return []byte{0xff, byte(v)}
	case v <= 0xffff:
		return []byte{0xfe, byte(v >> 8), byte(v)}
	case v <= 0xffffff:
		return []byte{0xfd, byte(v >> 16), byte(v >> 8), byte(v)}
	case v <= 0xffffffff:
		return []byte{0xfc, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	case v <= 0xffffffffff:
		return []byte{0xfb, byte(v >> 32), byte(v >> 24),
			byte(v >> 16), byte(v >> 8), byte(v)}
	case v <= 0xffffffffffff:
		return []byte{0xfa, byte(v >> 40), byte(v >> 32), byte(v >> 24),
			byte(v >> 16), byte(v >> 8), byte(v)}
	case v <= 0xffffffffffffff:
		return []byte{0xf9, byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	default:
		return []byte{0xf9, byte(v >> 56), byte(v >> 48), byte(v >> 40),
			byte(v >> 32), byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	}
}

func BinaryDecodeUint(input []byte) (v uint64, byteLen int, err error) {
	if len(input) == 0 {
		return 0, 0, verror.New(errEOF, nil)
	}
	firstByte := input[0]
	if firstByte <= 0x7f {
		return uint64(firstByte), 1, nil
	}

	if firstByte <= 0xdf {
		return 0, 0, verror.New(errInvalid, nil)
	}
	byteLen = int(-int8(firstByte))
	if byteLen < 1 || byteLen > uint64Size {
		return 0, 0, verror.New(errUintOverflow, nil)
	}
	if len(input) < byteLen {
		return 0, 0, verror.New(errEOF, nil)
	}
	for i := 1; i < byteLen; i++ {
		v = v<<8 | uint64(input[i])
	}
	return
}
