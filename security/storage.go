package security

import (
	"fmt"
	"io"

	"v.io/core/veyron/security/serialization"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vom"
)

func encodeAndStore(obj interface{}, data, signature io.WriteCloser, signer serialization.Signer) error {
	if data == nil || signature == nil {
		return fmt.Errorf("invalid data/signature handles data:%v sig:%v", data, signature)
	}
	swc, err := serialization.NewSigningWriteCloser(data, signature, signer, nil)
	if err != nil {
		return err
	}
	if err := vom.NewEncoder(swc).Encode(obj); err != nil {
		swc.Close()
		return err
	}
	return swc.Close()
}

func decodeFromStorage(obj interface{}, data, signature io.ReadCloser, publicKey security.PublicKey) error {
	if data == nil || signature == nil {
		return fmt.Errorf("invalid data/signature handles data:%v sig:%v", data, signature)
	}
	defer data.Close()
	defer signature.Close()
	vr, err := serialization.NewVerifyingReader(data, signature, publicKey)
	if err != nil {
		return err
	}
	return vom.NewDecoder(vr).Decode(obj)
}
