package security

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

const ecPrivateKeyPEMType = "EC PRIVATE KEY"

var nullACL security.ACL

// OpenACL creates an ACL that grants access to all principals.
func OpenACL() security.ACL {
	acl := security.ACL{}
	acl.In = map[security.BlessingPattern]security.LabelSet{security.AllPrincipals: security.AllLabels}
	return acl
}

var PassphraseErr = errors.New("passphrase incorrect for decrypting private key")

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

// LoadIdentity reads a PrivateID from r, assuming that it was written using
// SaveIdentity.
//
// TODO(ashankar): The extra arguments is a hack that is needed to keep identities
// generated before the "veyron.io" code move working with binaries built after.
// This hack should go away when we make the backward-incompatible change to the
// new security API anyway.
func LoadIdentity(r io.Reader, hack ...security.PrivateID) (security.PrivateID, error) {
	var id security.PrivateID
	if len(hack) > 0 {
		id = hack[0]
	}
	if err := vom.NewDecoder(base64.NewDecoder(base64.URLEncoding, r)).Decode(&id); err != nil {
		return nil, err
	}
	return id, nil
}

// SaveIdentity writes a serialized form of a PrivateID to w, which can be
// recovered using LoadIdentity.
func SaveIdentity(w io.Writer, id security.PrivateID) error {
	closer := base64.NewEncoder(base64.URLEncoding, w)
	if err := vom.NewEncoder(closer).Encode(id); err != nil {
		return err
	}
	// Must close the base64 encoder to flush out any partially written blocks.
	if err := closer.Close(); err != nil {
		return err
	}
	return nil
}

// LoadACL reads an ACL from the provided Reader containing a JSON encoded ACL.
func LoadACL(r io.Reader) (security.ACL, error) {
	var acl security.ACL
	if err := json.NewDecoder(r).Decode(&acl); err != nil {
		return nullACL, err
	}
	return acl, nil
}

// SaveACL encodes an ACL in JSON format and writes it to the provided Writer.
func SaveACL(w io.Writer, acl security.ACL) error {
	return json.NewEncoder(w).Encode(acl)
}

// CaveatValidators returns the set of security.CaveatValidators
// obtained by decoding the provided caveat bytes.
//
// It is an error if any of the provided caveat bytes cannot
// be decoded into a security.CaveatValidator.
func CaveatValidators(caveats ...security.Caveat) ([]security.CaveatValidator, error) {
	if len(caveats) == 0 {
		return nil, nil
	}
	validators := make([]security.CaveatValidator, len(caveats))
	for i, c := range caveats {
		var v security.CaveatValidator
		if err := vom.NewDecoder(bytes.NewReader(c.ValidatorVOM)).Decode(&v); err != nil {
			return nil, fmt.Errorf("caveat bytes could not be VOM-decoded: %s", err)
		}
		validators[i] = v
	}
	return validators, nil
}

// ThirdPartyCaveats returns the set of security.ThirdPartyCaveats
// that could be successfully decoded from the provided caveat bytes.
func ThirdPartyCaveats(caveats ...security.Caveat) []security.ThirdPartyCaveat {
	var tpCaveats []security.ThirdPartyCaveat
	for _, c := range caveats {
		var t security.ThirdPartyCaveat
		if err := vom.NewDecoder(bytes.NewReader(c.ValidatorVOM)).Decode(&t); err != nil {
			continue
		}
		tpCaveats = append(tpCaveats, t)
	}
	return tpCaveats
}
