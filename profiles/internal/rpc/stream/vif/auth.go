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
	// TODO(mattr): Temporarily don't do version negotiation over insecure channels.
	// This is because the current production agent is broken (see v.io/i/293).
	if params.LocalPrincipal == nil {
		return nullCipher, nil
	}

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
// TODO(mattr): I have temporarily added a new output parmeter message.T.
// If this message is non-nil then it is the first message that should be handled
// by the VIF.  This is needed because the first message we read here may
// or may not be a Setup message, if it's not we need to handle it in the
// message loop.  This parameter will be removed shortly.
func AuthenticateAsServer(writer io.Writer, reader *iobuf.Reader, versions *version.Range, principal security.Principal, lBlessings security.Blessings,
	dc vc.DischargeClient) (crypto.ControlCipher, message.T, error) {
	var err error
	if versions == nil {
		versions = version.SupportedRange
	}

	// Send server's public data.
	pvt, pub, err := makeSetup(versions, principal != nil)
	if err != nil {
		return nil, nil, err
	}

	errch := make(chan error, 1)
	readch := make(chan bool, 1)
	go func() {
		// TODO(mattr,ribrdb): In the case of the agent, which is
		// currently the only user of insecure connections, we need to
		// wait for the client to initiate the communication.  The agent
		// sends an extra first byte to clients, which clients read before
		// dialing their side of the vif.  If we send this message before
		// the magic byte has been sent the client will use the first
		// byte of this message instead rendering the remainder of the
		// stream uninterpretable.
		// TODO(mattr): As an even worse hack, we want to temporarily be
		// compatible with really old agents.  We can't send them our setup
		// message because they have a bug.  However we want new servers
		// to be compatible with clients that DO send the setup message.
		// To accomplish this we'll see if the client sends us a setup message
		// first.  If it does, we'll send ours.  If not, we wont.
		if principal != nil || <-readch {
			err := message.WriteTo(writer, pub, nullCipher)
			errch <- err
		}
		close(errch)
	}()

	// Read client's public data.
	pmsg, err := message.ReadFrom(reader, nullCipher)
	if err != nil {
		readch <- false
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}
	ppub, ok := pmsg.(*message.Setup)
	if !ok {
		// TODO(mattr): We got a message from the client, but it's not a Setup, so
		// assume this is a client that doesn't negotiate.  This should
		// be removed once we no longer have to be compatible with old agents.
		readch <- false
		return nullCipher, pmsg, nil
	} else {
		// TODO(mattr): We got a Setup message from the client, so
		// assume this is a client that does negotiate.  This should
		// be removed once we no longer have to be compatible with old agents.
		readch <- true
	}

	// Wait for the write to succeed.
	if err, ok := <-errch; !ok {
		// We didn't write because we didn't get a Setup message from the client.
		// Just return a nil cipher.  This basically just assumes the agent is
		// RPC version compatible.
		// TODO(mattr): This is part of the same hack described above, it should be
		// removed later.
		return nullCipher, nil, nil
	} else if err != nil {
		// The write failed, the VIF cannot be set up.
		return nil, nil, err
	}

	// Choose the max version in the intersection.
	vrange, err := versions.Intersect(&ppub.Versions)
	if err != nil {
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}
	v := vrange.Max

	if principal == nil {
		return nullCipher, nil, nil
	}

	// Perform authentication.
	cipher, err := authenticateAsServerRPC6(writer, reader, principal, lBlessings, dc, pvt, pub, ppub, v)
	return cipher, nil, err
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
