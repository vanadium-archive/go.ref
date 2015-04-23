// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"crypto/rand"
	"io"

	"golang.org/x/crypto/nacl/box"

	rpcversion "v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/ref/profiles/internal/lib/iobuf"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	"v.io/x/ref/profiles/internal/rpc/stream/message"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	"v.io/x/ref/profiles/internal/rpc/version"
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errAuthFailed                      = reg(".errAuthFailed", "authentication failed{:3}")
	errUnsupportedEncryptVersion       = reg(".errUnsupportedEncryptVersion", "unsupported encryption version {4} < {5}")
	errNaclBoxVersionNegotiationFailed = reg(".errNaclBoxVersionNegotiationFailed", "nacl box encryption version negotiation failed")
	errVersionNegotiationFailed        = reg(".errVersionNegotiationFailed", "encryption version negotiation failed")
	nullCipher                         crypto.NullControlCipher
)

// privateData includes secret data we need for encryption.
type privateData struct {
	naclBoxPrivateKey crypto.BoxKey
}

// AuthenticateAsClient sends a Setup message if possible.  If so, it chooses
// encryption based on the max supported version.
//
// The sequence is initiated by the client.
//
//    - The client sends a Setup message to the server, containing the client's
//      supported versions, and the client's crypto options.  The Setup message
//      is sent in the clear.
//
//    - When the server receives the Setup message, it calls
//      AuthenticateAsServer, which constructs a response Setup containing
//      the server's version range, and any crypto options.
//
//    - The client and server use the public/private key pairs
//      generated for the Setup messages to create an encrypted stream
//      of SetupStream messages for the remainder of the authentication
//      setup.  The encyrption uses NewControlCipherRPC6, which is based
//      on code.google.com/p/go.crypto/nacl/box.
//
//    - Once the encrypted SetupStream channel is setup, the client and
//      server authenticate using the vc.AuthenticateAs{Client,Server} protocol.
//
// Note that the Setup messages are sent in the clear, so they are subject to
// modification by a man-in-the-middle, which can currently force a downgrade by
// modifying the acceptable version ranges downward.  This can be addressed by
// including a hash of the Setup message in the encrypted stream.  It is
// likely that this will be addressed in subsequent protocol versions.
func AuthenticateAsClient(writer io.Writer, reader *iobuf.Reader, versions *version.Range, params security.CallParams, auth *vc.ServerAuthorizer) (crypto.ControlCipher, error) {
	if versions == nil {
		versions = version.SupportedRange
	}

	// Send the client's public data.
	pvt, pub, err := makeSetup(versions, params.LocalPrincipal != nil)
	if err != nil {
		return nil, verror.New(stream.ErrSecurity, nil, err)
	}

	errch := make(chan error, 1)
	go func() {
		errch <- message.WriteTo(writer, pub, nullCipher)
	}()

	pmsg, err := message.ReadFrom(reader, nullCipher)
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, err)
	}
	ppub, ok := pmsg.(*message.Setup)
	if !ok {
		return nil, verror.New(stream.ErrSecurity, nil, verror.New(errVersionNegotiationFailed, nil))
	}

	// Wait for the write to succeed.
	if err := <-errch; err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, err)
	}

	// Choose the max version in the intersection.
	vrange, err := pub.Versions.Intersect(&ppub.Versions)
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, err)
	}
	v := vrange.Max

	if params.LocalPrincipal == nil {
		return nullCipher, nil
	}

	// Perform the authentication.
	return authenticateAsClient(writer, reader, params, auth, pvt, pub, ppub, v)
}

func authenticateAsClient(writer io.Writer, reader *iobuf.Reader, params security.CallParams, auth *vc.ServerAuthorizer,
	pvt *privateData, pub, ppub *message.Setup, version rpcversion.RPCVersion) (crypto.ControlCipher, error) {
	pbox := ppub.NaclBox()
	if pbox == nil {
		return nil, verror.New(errNaclBoxVersionNegotiationFailed, nil)
	}
	c := crypto.NewControlCipherRPC6(&pbox.PublicKey, &pvt.naclBoxPrivateKey, false)
	sconn := newSetupConn(writer, reader, c)
	// TODO(jyh): act upon the authentication results.
	_, _, _, err := vc.AuthenticateAsClient(sconn, crypto.NewNullCrypter(), params, auth, version)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AuthenticateAsServer handles a Setup message, choosing authentication
// based on the max common version.
//
// See AuthenticateAsClient for a description of the negotiation.
func AuthenticateAsServer(writer io.Writer, reader *iobuf.Reader, versions *version.Range, principal security.Principal, lBlessings security.Blessings,
	dc vc.DischargeClient) (crypto.ControlCipher, error) {
	var err error
	if versions == nil {
		versions = version.SupportedRange
	}

	// Send server's public data.
	pvt, pub, err := makeSetup(versions, principal != nil)
	if err != nil {
		return nil, err
	}

	errch := make(chan error, 1)
	readch := make(chan struct{})
	go func() {
		// TODO(mattr,ribrdb): In the case of the agent, which is
		// currently the only user of insecure connections, we need to
		// wait for the client to initiate the communication.  The agent
		// sends an extra first byte to clients, which clients read before
		// dialing their side of the vif.  If we send this message before
		// the magic byte has been sent the client will use the first
		// byte of this message instead rendering the remainder of the
		// stream uninterpretable.
		if principal == nil {
			<-readch
		}
		err := message.WriteTo(writer, pub, nullCipher)
		errch <- err
	}()

	// Read client's public data.
	pmsg, err := message.ReadFrom(reader, nullCipher)
	close(readch) // Note: we need to close this whether we get an error or not.
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, err)
	}
	ppub, ok := pmsg.(*message.Setup)
	if !ok {
		return nil, verror.New(stream.ErrSecurity, nil, verror.New(errVersionNegotiationFailed, nil))
	}

	// Wait for the write to succeed.
	if err := <-errch; err != nil {
		return nil, err
	}

	// Choose the max version in the intersection.
	vrange, err := versions.Intersect(&ppub.Versions)
	if err != nil {
		return nil, verror.New(stream.ErrNetwork, nil, err)
	}
	v := vrange.Max

	if principal == nil {
		return nullCipher, nil
	}

	// Perform authentication.
	return authenticateAsServerRPC6(writer, reader, principal, lBlessings, dc, pvt, pub, ppub, v)
}

func authenticateAsServerRPC6(writer io.Writer, reader *iobuf.Reader, principal security.Principal, lBlessings security.Blessings, dc vc.DischargeClient,
	pvt *privateData, pub, ppub *message.Setup, version rpcversion.RPCVersion) (crypto.ControlCipher, error) {
	box := ppub.NaclBox()
	if box == nil {
		return nil, verror.New(errNaclBoxVersionNegotiationFailed, nil)
	}
	c := crypto.NewControlCipherRPC6(&box.PublicKey, &pvt.naclBoxPrivateKey, true)
	sconn := newSetupConn(writer, reader, c)
	// TODO(jyh): act upon authentication results.
	_, _, err := vc.AuthenticateAsServer(sconn, principal, lBlessings, dc, crypto.NewNullCrypter(), version)
	if err != nil {
		return nil, verror.New(errAuthFailed, nil, err)
	}
	return c, nil
}

// getDischargeClient returns the dischargeClient needed to fetch server discharges for this call.
// TODO(suharshs): Perhaps we should pass dischargeClient explicitly?
func getDischargeClient(lopts []stream.ListenerOpt) vc.DischargeClient {
	for _, o := range lopts {
		switch v := o.(type) {
		case vc.DischargeClient:
			return v
		}
	}
	return nil
}

// makeSetup constructs the options that this process can support.
func makeSetup(versions *version.Range, secure bool) (*privateData, *message.Setup, error) {
	var options []message.SetupOption
	var pvt *privateData
	if secure {
		pubKey, pvtKey, err := box.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		options = []message.SetupOption{&message.NaclBox{PublicKey: *pubKey}}
		pvt = &privateData{
			naclBoxPrivateKey: *pvtKey,
		}
	}

	pub := &message.Setup{
		Versions: *versions,
		Options:  options,
	}

	return pvt, pub, nil
}
