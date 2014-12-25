package principal

import (
	"encoding/base64"
	"v.io/veyron/veyron2/security"
)

type BlessingsHandle struct {
	Handle    int32
	PublicKey string
}

func ConvertBlessingsToHandle(blessings security.Blessings, handle int32) *BlessingsHandle {
	bytes, err := blessings.PublicKey().MarshalBinary()
	if err != nil {
		panic(err)
	}
	return &BlessingsHandle{
		Handle:    handle,
		PublicKey: base64.StdEncoding.EncodeToString(bytes),
	}
}
