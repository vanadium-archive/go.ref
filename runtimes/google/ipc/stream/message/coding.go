package message

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"veyron/runtimes/google/ipc/stream/id"
)

var errLargerThan3ByteUint = errors.New("integer too large to represent in 3 bytes")

func write3ByteUint(dst []byte, n int) error {
	if n >= (1<<24) || n < 0 {
		return errLargerThan3ByteUint
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

func readInt(r io.Reader, ptr interface{}) error {
	return binary.Read(r, binary.BigEndian, ptr)
}

func writeInt(w io.Writer, ptr interface{}) error {
	return binary.Write(w, binary.BigEndian, ptr)
}

func readString(r io.Reader, s *string) error {
	var size uint32
	if err := readInt(r, &size); err != nil {
		return err
	}
	bytes := make([]byte, size)
	n, err := r.Read(bytes)
	if err != nil {
		return err
	}
	if n != int(size) {
		return io.ErrUnexpectedEOF
	}
	*s = string(bytes)
	return nil
}

func writeString(w io.Writer, s string) error {
	size := uint32(len(s))
	if err := writeInt(w, size); err != nil {
		return err
	}
	n, err := w.Write([]byte(s))
	if err != nil {
		return err
	}
	if n != int(size) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// byteReader adapts an io.Reader to an io.ByteReader
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
		return 0, fmt.Errorf("read %d bytes, wanted to read 1", n)
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
