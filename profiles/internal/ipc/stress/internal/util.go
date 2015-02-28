package internal

import (
	"crypto/md5"
	"encoding/binary"

	"v.io/x/ref/profiles/internal/ipc/stress"
)

// doSum returns the MD5 checksum of the arg.
func doSum(arg stress.Arg) ([]byte, error) {
	h := md5.New()
	if arg.ABool {
		if err := binary.Write(h, binary.LittleEndian, arg.AInt64); err != nil {
			return nil, err
		}
	}
	if _, err := h.Write(arg.AListOfBytes); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
