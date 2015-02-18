package principal

import (
	"encoding/base64"
	"v.io/core/veyron2/security"
)

type BlessingsHandle struct {
	Handle    int32
	PublicKey string
}

func ConvertBlessingsToHandle(blessings security.Blessings, handle int32) *BlessingsHandle {
	encoded, err := EncodePublicKey(blessings.PublicKey())
	if err != nil {
		panic(err)
	}
	return &BlessingsHandle{
		Handle:    handle,
		PublicKey: encoded,
	}
}

func EncodePublicKey(key security.PublicKey) (string, error) {
	bytes, err := key.MarshalBinary()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}
