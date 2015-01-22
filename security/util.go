package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vom"
)

const ecPrivateKeyPEMType = "EC PRIVATE KEY"

var PassphraseErr = errors.New("passphrase incorrect for decrypting private key")

// NewPrincipalKey generates an ECDSA (public, private) key pair.
func NewPrincipalKey() (security.PublicKey, *ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return security.NewECDSAPublicKey(&priv.PublicKey), priv, nil
}

// LoadPEMKey loads a key from 'r'. returns PassphraseErr for incorrect Passphrase.
// If the key held in 'r' is unencrypted, 'passphrase' will be ignored.
func LoadPEMKey(r io.Reader, passphrase []byte) (interface{}, error) {
	pemBlockBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	pemBlock, _ := pem.Decode(pemBlockBytes)
	if pemBlock == nil {
		return nil, errors.New("no PEM key block read")
	}
	var data []byte
	if x509.IsEncryptedPEMBlock(pemBlock) {
		data, err = x509.DecryptPEMBlock(pemBlock, passphrase)
		if err != nil {
			return nil, PassphraseErr
		}
	} else {
		data = pemBlock.Bytes
	}

	switch pemBlock.Type {
	case ecPrivateKeyPEMType:
		key, err := x509.ParseECPrivateKey(data)
		if err != nil {
			return nil, PassphraseErr
		}
		return key, nil
	}
	return nil, fmt.Errorf("PEM key block has an unrecognized type: %v", pemBlock.Type)
}

// SavePEMKey marshals 'key', encrypts it using 'passphrase', and saves the bytes to 'w' in PEM format.
// If passphrase is nil, the key will not be encrypted.
//
// For example, if key is an ECDSA private key, it will be marshaled
// in ASN.1, DER format, encrypted, and then written in a PEM block.
func SavePEMKey(w io.Writer, key interface{}, passphrase []byte) error {
	var data []byte
	var err error
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		if data, err = x509.MarshalECPrivateKey(k); err != nil {
			return err
		}
	default:
		return fmt.Errorf("key of type %T cannot be saved", k)
	}

	var pemKey *pem.Block
	if passphrase != nil {
		pemKey, err = x509.EncryptPEMBlock(rand.Reader, ecPrivateKeyPEMType, data, passphrase, x509.PEMCipherAES256)
		if err != nil {
			return fmt.Errorf("failed to encrypt pem block: %v", err)
		}
	} else {
		pemKey = &pem.Block{
			Type:  ecPrivateKeyPEMType,
			Bytes: data,
		}
	}

	return pem.Encode(w, pemKey)
}

// ThirdPartyCaveats returns the set of security.ThirdPartyCaveats
// that could be successfully decoded from the provided caveat bytes.
func ThirdPartyCaveats(caveats ...security.Caveat) []security.ThirdPartyCaveat {
	var tpCaveats []security.ThirdPartyCaveat
	for _, c := range caveats {
		var t security.ThirdPartyCaveat
		if err := vom.Decode(c.ValidatorVOM, &t); err != nil {
			continue
		}
		tpCaveats = append(tpCaveats, t)
	}
	return tpCaveats
}
