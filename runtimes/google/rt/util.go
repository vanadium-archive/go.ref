package rt

import (
	"io/ioutil"
	"os"
	"path"

	"veyron.io/veyron/veyron/security/serialization"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

func encodeAndStore(obj interface{}, dir, dataFile, sigFile string, signer serialization.Signer) error {
	// Save the object to temporary data and signature files, and then move
	// those files to the actual data and signature file. This reduces the
	// risk of loosing all saved data on disk in the event of a Write failure.
	data, err := ioutil.TempFile(dir, "data")
	if err != nil {
		return err
	}
	defer os.Remove(data.Name())
	sig, err := ioutil.TempFile(dir, "sig")
	if err != nil {
		return err
	}
	defer os.Remove(sig.Name())

	swc, err := serialization.NewSigningWriteCloser(data, sig, signer, nil)
	if err != nil {
		return err
	}
	if err := vom.NewEncoder(swc).Encode(obj); err != nil {
		swc.Close()
		return err
	}
	if err := swc.Close(); err != nil {
		return err
	}

	if err := os.Rename(data.Name(), path.Join(dir, dataFile)); err != nil {
		return err
	}
	return os.Rename(sig.Name(), path.Join(dir, sigFile))
}

func decodeFromStorage(obj interface{}, dir, dataFile, sigFile string, publicKey security.PublicKey) error {
	data, dataErr := os.Open(path.Join(dir, dataFile))
	defer data.Close()
	sig, sigErr := os.Open(path.Join(dir, sigFile))
	defer sig.Close()

	switch {
	case os.IsNotExist(dataErr) && os.IsNotExist(sigErr):
		return nil
	case dataErr != nil:
		return dataErr
	case sigErr != nil:
		return sigErr
	}

	vr, err := serialization.NewVerifyingReader(data, sig, publicKey)
	if err != nil {
		return err
	}
	return vom.NewDecoder(vr).Decode(obj)
}
