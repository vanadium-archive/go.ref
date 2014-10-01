package vc

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/crypto"
	"veyron.io/veyron/veyron/runtimes/google/lib/iobuf"

	"veyron.io/veyron/veyron2/ipc/version"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

const (
	clientChannelEnd = "client"
	serverChannelEnd = "server"
)

var (
	authServerContextTag = []byte("VCauthS\x00")
	authClientContextTag = []byte("VCauthC\x00")
)

// authenticateAsServer executes the authentication protocol at the server and
// returns the identity of the client and server.
func authenticateAsServerOld(conn io.ReadWriteCloser, localID LocalID, crypter crypto.Crypter, v version.IPCVersion) (clientID, serverID security.PublicID, err error) {
	// The authentication protocol has the server doing the final read, so
	// it is the one that closes the connection.
	defer conn.Close()
	if serverID, err = localID.AsServer(); err != nil {
		return
	}
	if err = writeIdentity(conn, serverChannelEnd, authServerContextTag, crypter, localID, serverID, v); err != nil {
		return
	}
	clientID, err = readIdentity(conn, clientChannelEnd, authClientContextTag, crypter, v)
	return
}

// authenticateAsClient executes the authentication protocol at the client and
// returns the identity of the server.
//
// If serverName is non-nil, the authentication protocol will be considered
// successfull iff the server identity matches the provided regular expression.
func authenticateAsClientOld(conn io.ReadWriteCloser, localID LocalID, crypter crypto.Crypter, v version.IPCVersion) (serverID, clientID security.PublicID, err error) {
	defer conn.Close()
	if serverID, err = readIdentity(conn, serverChannelEnd, authServerContextTag, crypter, v); err != nil {
		return
	}
	if clientID, err = localID.AsClient(serverID); err != nil {
		return
	}
	err = writeIdentity(conn, clientChannelEnd, authClientContextTag, crypter, localID, clientID, v)
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
	errInvalidSignatureInMessage = errors.New("signature does not verify in authentication handshake message")
	errSingleCertificateRequired = errors.New("exactly one X.509 certificate chain with exactly one certificate is required")
)

func writeIdentity(w io.Writer, chEnd string, contextTag []byte, crypter crypto.Crypter, id LocalID, pub security.PublicID, v version.IPCVersion) error {
	// TODO(ashankar): Remove references to protocol V1 and V2 before release
	if v != version.UnknownIPCVersion && v < version.IPCVersion2 {
		return fmt.Errorf("cannot authenticate with deprecated RPC protocol version(%v)", v)
	}
	if v == version.IPCVersion2 {
		// Compute channel id - encrypted chEnd string
		chid, err := crypter.Encrypt(iobuf.NewSlice([]byte(chEnd)))
		if err != nil {
			return err
		}
		defer chid.Release()

		// VOM-encode and encrypt the (public) identity.
		var buf bytes.Buffer
		if err := vom.NewEncoder(&buf).Encode(pub); err != nil {
			return err
		}
		eid, err := crypter.Encrypt(iobuf.NewSlice(buf.Bytes()))
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
		buf = bytes.Buffer{}
		if err := vom.NewEncoder(&buf).Encode(signature); err != nil {
			return err
		}
		esig, err := crypter.Encrypt(iobuf.NewSlice(buf.Bytes()))
		if err != nil {
			return err
		}
		defer esig.Release()
		msg.Signature = esig.Contents
		// Write the message out.
		return vom.NewEncoder(w).Encode(msg)
	}
	// v == version.IPCVersion3
	signature, err := id.Sign(append(contextTag, crypter.ChannelBinding()...))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	ve := vom.NewEncoder(&buf)
	if err := ve.Encode(pub); err != nil {
		return err
	}
	if err := ve.Encode(signature); err != nil {
		return err
	}
	msg, err := crypter.Encrypt(iobuf.NewSlice(buf.Bytes()))
	if err != nil {
		return err
	}
	defer msg.Release()
	return vom.NewEncoder(w).Encode(msg.Contents)
}

func readIdentity(reader io.Reader, expectedChEnd string, contextTag []byte, crypter crypto.Crypter, v version.IPCVersion) (security.PublicID, error) {
	if v != version.UnknownIPCVersion && v < version.IPCVersion2 {
		return nil, fmt.Errorf("cannot authenticate with deprecated RPC protocol version(%v)", v)
	}
	if v == version.IPCVersion2 {
		// Read the message.
		var msg identityMessage
		if err := vom.NewDecoder(reader).Decode(&msg); err != nil {
			return nil, fmt.Errorf("failed to decode identityMessage: %v", err)
		}

		// Decrypt and authenticate the channel id
		chEnd, err := crypter.Decrypt(iobuf.NewSlice(msg.ChannelID))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt ChannelID: %v", err)
		}
		defer chEnd.Release()
		if bytes.Compare([]byte(expectedChEnd), chEnd.Contents) != 0 {
			return nil, errChannelIDMismatch
		}

		// Decrypt and VOM-decode the identity
		idbytes, err := crypter.Decrypt(iobuf.NewSlice(msg.ID))
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
		sigbytes, err := crypter.Decrypt(iobuf.NewSlice(msg.Signature))
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
		return id, nil
	}
	// v == version.IPCVersion3
	var encryptedHandshake []byte
	if err := vom.NewDecoder(reader).Decode(&encryptedHandshake); err != nil {
		return nil, fmt.Errorf("failed to read handshake: %v", err)
	}
	handshake, err := crypter.Decrypt(iobuf.NewSlice(encryptedHandshake))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt handshake: %v", err)
	}
	defer handshake.Release()
	vd := vom.NewDecoder(bytes.NewBuffer(handshake.Contents))
	var id security.PublicID
	if err := vd.Decode(&id); err != nil {
		return nil, fmt.Errorf("failed to decode security.PublicID: %v", err)
	}
	var signature security.Signature
	if err := vd.Decode(&signature); err != nil {
		return nil, fmt.Errorf("failed to decode security.Signature: %v", err)
	}
	if !signature.Verify(id.PublicKey(), append(contextTag, crypter.ChannelBinding()...)) {
		return nil, errInvalidSignatureInMessage
	}
	return id, nil
}

// authenticateAsServer executes the authentication protocol at the server and
// returns the blessings used to authenticate both ends.
func authenticateAsServer(conn io.ReadWriteCloser, principal security.Principal, crypter crypto.Crypter, v version.IPCVersion) (client, server security.Blessings, err error) {
	defer conn.Close()
	server = principal.BlessingStore().Default()
	if server == nil {
		return nil, nil, fmt.Errorf("BlessingStore does not contain a default set of blessings, cannot act as a server")
	}
	if err := writeBlessings(conn, authServerContextTag, crypter, principal, server, v); err != nil {
		return nil, nil, err
	}
	if client, err = readBlessings(conn, authClientContextTag, crypter, v); err != nil {
		return nil, nil, err
	}
	return client, server, nil
}

// authenticateAsClient executes the authentication protocol at the client and
// returns the blessings used to authenticate both ends.
//
// The client will only share its identity if its blessing store has one marked
// for the server (who shares its blessings first).
//
// TODO(ashankar): Seems like there is no way the blessing store
// can say that it does NOT want to share the default blessing with the server?
func authenticateAsClient(conn io.ReadWriteCloser, principal security.Principal, crypter crypto.Crypter, v version.IPCVersion) (server, client security.Blessings, err error) {
	defer conn.Close()
	if server, err = readBlessings(conn, authServerContextTag, crypter, v); err != nil {
		return nil, nil, err
	}
	serverB := server.ForContext(&serverAuthContext{
		self:   principal,
		remote: server,
		// TODO(ashankar): Get the local and remote endpoint here?
		// There is also a bootstrapping problem here. For example, let's say
		// (1) server has the blessing "provider/server" with a PeerIdentity caveat of "provider/client"
		// (2) Client has a blessing "provider/client" tagged for "provider/server" in its BlessingStore
		// How do we get that working?
		// One option is to have a UnionOfBlessings of all blessings of the client in the BlessingStore
		// made available to serverAuthContext.LocalBlessings for this call.
	})
	client = principal.BlessingStore().ForPeer(serverB...)
	if client == nil {
		return nil, nil, fmt.Errorf("No blessing tagged for peer %v in the BlessingStore", serverB)
	}
	if err = writeBlessings(conn, authClientContextTag, crypter, principal, client, v); err != nil {
		return nil, nil, err
	}
	return server, client, nil
}

func writeBlessings(w io.Writer, tag []byte, crypter crypto.Crypter, p security.Principal, b security.Blessings, v version.IPCVersion) error {
	signature, err := p.Sign(append(tag, crypter.ChannelBinding()...))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := vom.NewEncoder(&buf)
	if err := enc.Encode(signature); err != nil {
		return err
	}
	if err := enc.Encode(b); err != nil {
		return err
	}
	msg, err := crypter.Encrypt(iobuf.NewSlice(buf.Bytes()))
	if err != nil {
		return err
	}
	defer msg.Release()
	return vom.NewEncoder(w).Encode(msg.Contents)
}

func readBlessings(r io.Reader, tag []byte, crypter crypto.Crypter, v version.IPCVersion) (security.Blessings, error) {
	var msg []byte
	if err := vom.NewDecoder(r).Decode(&msg); err != nil {
		return nil, fmt.Errorf("failed to read handshake message: %v", err)
	}
	buf, err := crypter.Decrypt(iobuf.NewSlice(msg))
	if err != nil {
		return nil, err
	}
	defer buf.Release()
	dec := vom.NewDecoder(bytes.NewReader(buf.Contents))
	var wireb security.WireBlessings
	var sig security.Signature
	if err := dec.Decode(&sig); err != nil {
		return nil, err
	}
	if err := dec.Decode(&wireb); err != nil {
		return nil, err
	}
	b, err := security.NewBlessings(wireb)
	if err != nil {
		return nil, err
	}
	if !sig.Verify(b.PublicKey(), append(tag, crypter.ChannelBinding()...)) {
		return nil, errInvalidSignatureInMessage
	}
	return b, nil
}

// security.Context implementation used when extracting blessings from what the
// server presents during authentication.
type serverAuthContext struct {
	self   security.Principal
	remote security.Blessings
}

func (*serverAuthContext) Method() string                              { return "" }
func (*serverAuthContext) Name() string                                { return "" }
func (*serverAuthContext) Suffix() string                              { return "" }
func (*serverAuthContext) Label() security.Label                       { return security.ReadLabel }
func (c *serverAuthContext) Discharges() map[string]security.Discharge { return nil }
func (c *serverAuthContext) LocalPrincipal() security.Principal        { return c.self }
func (c *serverAuthContext) LocalBlessings() security.Blessings        { return nil }
func (c *serverAuthContext) RemoteBlessings() security.Blessings       { return c.remote }
func (c *serverAuthContext) LocalID() security.PublicID                { return nil }
func (c *serverAuthContext) RemoteID() security.PublicID               { return nil }
func (c *serverAuthContext) LocalEndpoint() naming.Endpoint            { return nil }
func (c *serverAuthContext) RemoteEndpoint() naming.Endpoint           { return nil }
