package vc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/crypto"
	"veyron.io/veyron/veyron/runtimes/google/lib/iobuf"

	"veyron.io/veyron/veyron2/ipc/version"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

var (
	authServerContextTag = []byte("VCauthS\x00")
	authClientContextTag = []byte("VCauthC\x00")
)

var (
	errSameChannelPublicKey      = errors.New("same public keys for both ends of the channel")
	errChannelIDMismatch         = errors.New("channel id does not match expectation")
	errInvalidSignatureInMessage = errors.New("signature does not verify in authentication handshake message")
	errNoCertificatesReceived    = errors.New("no certificates received")
	errSingleCertificateRequired = errors.New("exactly one X.509 certificate chain with exactly one certificate is required")
)

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
		self:      principal,
		remote:    server,
		timestamp: time.Now(),
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
		return nil, nil, fmt.Errorf("no blessing tagged for peer %v in the BlessingStore", serverB)
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
	if b == nil {
		return nil, errNoCertificatesReceived
	}
	if !sig.Verify(b.PublicKey(), append(tag, crypter.ChannelBinding()...)) {
		return nil, errInvalidSignatureInMessage
	}
	return b, nil
}

// security.Context implementation used when extracting blessings from what the
// server presents during authentication.
type serverAuthContext struct {
	self      security.Principal
	remote    security.Blessings
	timestamp time.Time
}

func (c *serverAuthContext) Timestamp() time.Time                      { return c.timestamp }
func (*serverAuthContext) Method() string                              { return "" }
func (*serverAuthContext) MethodTags() []interface{}                   { return nil }
func (*serverAuthContext) Name() string                                { return "" }
func (*serverAuthContext) Suffix() string                              { return "" }
func (*serverAuthContext) Label() (l security.Label)                   { return l }
func (c *serverAuthContext) Discharges() map[string]security.Discharge { return nil }
func (c *serverAuthContext) LocalPrincipal() security.Principal        { return c.self }
func (c *serverAuthContext) LocalBlessings() security.Blessings        { return nil }
func (c *serverAuthContext) RemoteBlessings() security.Blessings       { return c.remote }
func (c *serverAuthContext) LocalEndpoint() naming.Endpoint            { return nil }
func (c *serverAuthContext) RemoteEndpoint() naming.Endpoint           { return nil }
