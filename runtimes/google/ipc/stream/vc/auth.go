package vc

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io"
	"math/big"

	"veyron/runtimes/google/ipc/stream/crypto"
	"veyron/runtimes/google/lib/iobuf"

	"veyron2/ipc/version"
	"veyron2/security"
	"veyron2/vom"
)

const (
	clientChannelEnd = "client"
	serverChannelEnd = "server"
)

// authenticateAsServer executes the authentication protocol at the server and
// returns the identity of the client and server.
func authenticateAsServer(conn io.ReadWriteCloser, localID LocalID, crypter crypto.Crypter, v version.IPCVersion) (clientID, serverID security.PublicID, err error) {
	// The authentication protocol has the server doing the final read, so
	// it is the one that closes the connection.
	defer conn.Close()
	if serverID, err = localID.AsServer(); err != nil {
		return
	}
	if err = writeIdentity(conn, serverChannelEnd, crypter, localID, serverID, v); err != nil {
		return
	}
	clientID, err = readIdentity(conn, clientChannelEnd, crypter, v)
	return
}

// authenticateAsClient executes the authentication protocol at the client and
// returns the identity of the server.
//
// If serverName is non-nil, the authentication protocol will be considered
// successfull iff the server identity matches the provided regular expression.
func authenticateAsClient(conn io.ReadWriteCloser, localID LocalID, crypter crypto.Crypter, v version.IPCVersion) (serverID, clientID security.PublicID, err error) {
	defer conn.Close()
	if serverID, err = readIdentity(conn, serverChannelEnd, crypter, v); err != nil {
		return
	}
	// TODO(ashankar,ataly): Have the ability to avoid talking to a server we do not want to.
	// Will require calling Authorize on the server id?
	if clientID, err = localID.AsClient(serverID); err != nil {
		return
	}
	err = writeIdentity(conn, clientChannelEnd, crypter, localID, clientID, v)
	return
}

// identityMessage encapsulates information sent across the wire by a principal
// to introduce itself.
//
// Each field in the message is encrypted by the sender.
type identityMessage struct {
	ChannelID []byte
	ID        []byte // VOM-encoded
	SignR     []byte
	SignS     []byte
	Signature []byte // VOM-encoded
}

var (
	errSameChannelPublicKey      = errors.New("same public keys for both ends of the channel")
	errChannelIDMismatch         = errors.New("channel id does not match expectation")
	errInvalidIdentityInMessage  = errors.New("invalid identity in authentication message")
	errInvalidSignatureInMessage = errors.New("channel id signature does not match in authentication message")
	errSingleCertificateRequired = errors.New("exactly one X.509 certificate chain with exactly one certificate is required")
)

func writeIdentity(w io.Writer, chEnd string, enc crypto.Encrypter, id LocalID, pub security.PublicID, v version.IPCVersion) error {
	// Compute channel id - encrypted chEnd string
	chid, err := enc.Encrypt(iobuf.NewSlice([]byte(chEnd)))
	if err != nil {
		return err
	}
	defer chid.Release()

	// VOM-encode and encrypt the (public) identity.
	var buf bytes.Buffer
	if err := vom.NewEncoder(&buf).Encode(pub); err != nil {
		return err
	}
	eid, err := enc.Encrypt(iobuf.NewSlice(buf.Bytes()))
	if err != nil {
		return err
	}
	defer eid.Release()

	// Sign the channel ID
	signature, err := id.Sign(chid.Contents)
	if err != nil {
		return err
	}
	msg := identityMessage{
		ID:        eid.Contents,
		ChannelID: chid.Contents,
	}
	if v == version.IPCVersion1 {
		er, err := enc.Encrypt(iobuf.NewSlice(signature.R))
		if err != nil {
			return err
		}
		defer er.Release()
		es, err := enc.Encrypt(iobuf.NewSlice(signature.S))
		if err != nil {
			return err
		}
		defer es.Release()
		msg.SignR, msg.SignS = er.Contents, es.Contents
	} else {
		buf = bytes.Buffer{}
		if err := vom.NewEncoder(&buf).Encode(signature); err != nil {
			return err
		}
		esig, err := enc.Encrypt(iobuf.NewSlice(buf.Bytes()))
		if err != nil {
			return err
		}
		defer esig.Release()
		msg.Signature = esig.Contents
	}

	// Write the message out.
	return vom.NewEncoder(w).Encode(msg)
}

func readIdentity(reader io.Reader, expectedChEnd string, dec crypto.Decrypter, v version.IPCVersion) (security.PublicID, error) {
	// Read the message.
	var msg identityMessage
	if err := vom.NewDecoder(reader).Decode(&msg); err != nil {
		return nil, fmt.Errorf("failed to decode identityMessage: %v", err)
	}

	// Decrypt and authenticate the channel id
	chEnd, err := dec.Decrypt(iobuf.NewSlice(msg.ChannelID))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt ChannelID: %v", err)
	}
	defer chEnd.Release()
	if bytes.Compare([]byte(expectedChEnd), chEnd.Contents) != 0 {
		return nil, errChannelIDMismatch
	}

	// Decrypt and VOM-decode the identity
	idbytes, err := dec.Decrypt(iobuf.NewSlice(msg.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt security.PublicID: %v", err)
	}
	defer idbytes.Release()
	var id security.PublicID
	if err := vom.NewDecoder(bytes.NewBuffer(idbytes.Contents)).Decode(&id); err != nil {
		return nil, fmt.Errorf("failed to decode security.PublicID: %v", err)
	}
	if id == nil || id.PublicKey() == nil {
		return nil, errInvalidIdentityInMessage
	}

	// Decrypt the signature
	if v == version.IPCVersion1 {
		r, err := dec.Decrypt(iobuf.NewSlice(msg.SignR))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt SignR: %v", err)
		}
		defer r.Release()
		s, err := dec.Decrypt(iobuf.NewSlice(msg.SignS))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt SignS: %v", err)
		}
		defer s.Release()
		if !ecdsa.Verify(id.PublicKey(), msg.ChannelID, bigInt(r), bigInt(s)) {
			return nil, errInvalidSignatureInMessage
		}
	} else {
		sigbytes, err := dec.Decrypt(iobuf.NewSlice(msg.Signature))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt Signature: %v", err)
		}
		defer sigbytes.Release()
		var sig security.Signature
		if err = vom.NewDecoder(bytes.NewBuffer(sigbytes.Contents)).Decode(&sig); err != nil {
			return nil, fmt.Errorf("failed to decode security.Signature: %v", err)
		}
		// Validate the signature
		if !sig.Verify(id.PublicKey(), msg.ChannelID) {
			return nil, errInvalidSignatureInMessage
		}
	}

	return id, nil
}

func bigInt(slice *iobuf.Slice) *big.Int { return new(big.Int).SetBytes(slice.Contents) }
