// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package message

import (
	"encoding/binary"
	"io"

	"v.io/v23/verror"

	"v.io/x/ref/runtime/internal/rpc/stream/id"
)

const pkgPath = "v.io/x/ref/runtime/internal/rpc/stream/message"

func reg(id, msg string) verror.IDAction {
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errLargerThan3ByteUint = reg(".errLargerThan3ByteUnit", "integer too large to represent in 3 bytes")
	errReadWrongNumBytes   = reg(".errReadWrongNumBytes", "read {3} bytes, wanted to read {4}")

	sizeOfSizeT int // How many bytes are used to encode the type sizeT.
)

type sizeT uint32

func init() {
	sizeOfSizeT = binary.Size(sizeT(0))
}

func write3ByteUint(dst []byte, n int) error {
	if n >= (1<<24) || n < 0 {
		return verror.New(errLargerThan3ByteUint, nil)
	}
	dst[0] = byte((n & 0xff0000) >> 16)
	dst[1] = byte((n & 0x00ff00) >> 8)
	dst[2] = byte(n & 0x0000ff)
	return nil
}

func read3ByteUint(src []byte) int {
	return int(src[0])<<16 | int(src[1])<<8 | int(src[2])
}

func write4ByteUint(dst []byte, n uint32) {
	dst[0] = byte((n & 0xff000000) >> 24)
	dst[1] = byte((n & 0x00ff0000) >> 16)
	dst[2] = byte((n & 0x0000ff00) >> 8)
	dst[3] = byte(n & 0x000000ff)
}

func read4ByteUint(src []byte) uint32 {
	return uint32(src[0])<<24 | uint32(src[1])<<16 | uint32(src[2])<<8 | uint32(src[3])
}

func readInt(r io.Reader, i interface{}) error {
	return binary.Read(r, binary.BigEndian, i)
}

func writeInt(w io.Writer, i interface{}) error {
	return binary.Write(w, binary.BigEndian, i)
}

func readString(r io.Reader) (string, error) {
	b, err := readBytes(r)
	return string(b), err
}

func writeString(w io.Writer, s string) error {
	return writeBytes(w, []byte(s))
}

func readBytes(r io.Reader) ([]byte, error) {
	var size sizeT
	if err := readInt(r, &size); err != nil {
		return nil, err
	}
	b := make([]byte, size)
	n, err := r.Read(b)
	if err != nil {
		return nil, err
	}
	if n != int(size) {
		return nil, verror.New(errReadWrongNumBytes, nil, n, int(size))
	}
	return b, nil
}

func writeBytes(w io.Writer, b []byte) error {
	size := sizeT(len(b))
	if err := writeInt(w, size); err != nil {
		return err
	}
	n, err := w.Write(b)
	if err != nil {
		return err
	}
	if n != int(size) {
		return verror.New(errReadWrongNumBytes, nil, n, int(size))
	}
	return nil
}

// byteReader adapts an io.Reader to an io.ByteReader so that we can
// use it with encoding/Binary for varint etc.
type byteReader struct{ io.Reader }

func (b byteReader) ReadByte() (byte, error) {
	var buf [1]byte
	n, err := b.Reader.Read(buf[:])
	switch {
	case n == 1:
		return buf[0], err
	case err != nil:
		return 0, err
	default:
		return 0, verror.New(errReadWrongNumBytes, nil, n, 1)
	}
}

func readCounters(r io.Reader) (Counters, error) {
	var br io.ByteReader
	var ok bool
	if br, ok = r.(io.ByteReader); !ok {
		br = byteReader{r}
	}
	size, err := binary.ReadUvarint(br)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	c := Counters(make(map[CounterID]uint32, size))
	for i := uint64(0); i < size; i++ {
		vci, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, err
		}
		fid, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, err
		}
		bytes, err := binary.ReadUvarint(br)
		if err != nil {
			return nil, err
		}
		c.Add(id.VC(vci), id.Flow(fid), uint32(bytes))
	}
	return c, nil
}

func writeCounters(w io.Writer, c Counters) (err error) {
	var vbuf [binary.MaxVarintLen64]byte
	putUvarint := func(n uint64) {
		if err == nil {
			_, err = w.Write(vbuf[:binary.PutUvarint(vbuf[:], n)])
		}
	}
	putUvarint(uint64(len(c)))
	for cid, bytes := range c {
		putUvarint(uint64(cid.VCI()))
		putUvarint(uint64(cid.Flow()))
		putUvarint(uint64(bytes))
	}
	return
}

func readSetupOptions(r io.Reader) ([]SetupOption, error) {
	var opts []SetupOption
	for {
		var code setupOptionCode
		switch err := readInt(r, &code); err {
		case io.EOF:
			return opts, nil
		case nil:
			break
		default:
			return nil, err
		}
		var size uint16
		if err := readInt(r, &size); err != nil {
			return nil, err
		}
		l := &io.LimitedReader{R: r, N: int64(size)}
		switch code {
		case naclBoxOptionCode:
			var opt NaclBox
			if err := opt.read(l); err != nil {
				return nil, err
			}
			opts = append(opts, &opt)
		case peerEndpointOptionCode:
			var opt PeerEndpoint
			if err := opt.read(l); err != nil {
				return nil, err
			}
			opts = append(opts, &opt)
		case useVIFAuthenticationOptionCode:
			var opt UseVIFAuthentication
			if err := opt.read(l); err != nil {
				return nil, err
			}
			opts = append(opts, &opt)
		}
		// Consume any data remaining.
		readAndDiscardToError(l)
	}
}

func writeSetupOptions(w io.Writer, options []SetupOption) error {
	for _, opt := range options {
		if err := writeInt(w, opt.code()); err != nil {
			return err
		}
		if err := writeInt(w, opt.size()); err != nil {
			return err
		}
		if err := opt.write(w); err != nil {
			return err
		}
	}
	return nil
}

func readAndDiscardToError(r io.Reader) {
	var data [1024]byte
	for {
		if _, err := r.Read(data[:]); err != nil {
			return
		}
	}
}
