package lib

import (
	"bytes"
	"encoding/hex"
	"veyron.io/veyron/veyron2/vom2"
)

func VomEncode(v interface{}) (string, error) {
	var buf bytes.Buffer
	encoder, err := vom2.NewBinaryEncoder(&buf)
	if err != nil {
		return "", err
	}

	if err := encoder.Encode(v); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf.Bytes()), nil
}
