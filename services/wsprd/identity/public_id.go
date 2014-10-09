package identity

import (
	"encoding/base64"
	"veyron.io/veyron/veyron2/security"
)

type PublicIDHandle struct {
	Handle    int64
	PublicKey string
	Names     []string
}

func ConvertPublicIDToHandle(id security.PublicID, handle int64) *PublicIDHandle {
	bytes, err := id.PublicKey().MarshalBinary()
	if err != nil {
		panic(err)
	}
	return &PublicIDHandle{
		Handle:    handle,
		PublicKey: base64.StdEncoding.EncodeToString(bytes),
		Names:     id.Names(),
	}
}
