// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"io"

	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/ref/runtime/internal/lib/iobuf"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
	"v.io/x/ref/runtime/internal/rpc/stream/message"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	iversion "v.io/x/ref/runtime/internal/rpc/version"
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

// AuthenticationResult includes the result of the VIF authentication.
type AuthenticationResult struct {
	Dialed           bool
	Version          version.RPCVersion
	RemoteEndpoint   naming.Endpoint
	SessionKeys      SessionKeys
	LocalBlessings   security.Blessings
	RemoteBlessings  security.Blessings
	LocalDischarges  map[string]security.Discharge
	RemoteDischarges map[string]security.Discharge
}

// Public/private keys used to establish an encrypted communication channel
// (and not the keys corresponding to the principal used in authentication).
type SessionKeys struct {
	LocalPublic, LocalPrivate, RemotePublic crypto.BoxKey
}

// AuthenticateAsClient sends a Setup message if possible. If so, it chooses
// encryption based on the max supported version.
//
// The sequence is initiated by the client.
//
//    - The client sends a Setup message to the server, containing the client's
//      supported versions, and the client's crypto options. The Setup message
//      is sent in the clear.
//
//    - When the server receives the Setup message, it calls
//      AuthenticateAsServer, which constructs a response Setup containing
//      the server's version range, and any crypto options.
//
//    - The client and server use the public/private key pairs
//      generated for the Setup messages to create an encrypted stream
//      of SetupStream messages for the remainder of the authentication
//      setup. The encryption uses NewControlCipherRPC11, which is based
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
func AuthenticateAsClient(writer io.Writer, reader *iobuf.Reader, localEP naming.Endpoint, versions *iversion.Range, principal security.Principal, auth *vc.ServerAuthorizer) (crypto.ControlCipher, *AuthenticationResult, error) {
	if versions == nil {
		versions = iversion.SupportedRange
	}

	// Send the client's public data.
	setup, pk, sk, err := makeSetup(versions, localEP)
	if err != nil {
		return nil, nil, verror.New(stream.ErrSecurity, nil, err)
	}

	errch := make(chan error, 1)
	go func() {
		errch <- message.WriteTo(writer, setup, nullCipher)
	}()

	remoteMsg, err := message.ReadFrom(reader, nullCipher)
	if err != nil {
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}
	remoteSetup, ok := remoteMsg.(*message.Setup)
	if !ok {
		return nil, nil, verror.New(stream.ErrSecurity, nil, verror.New(errVersionNegotiationFailed, nil))
	}

	// Wait for the write to succeed.
	if err := <-errch; err != nil {
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}

	// Choose the max version in the intersection.
	vrange, err := setup.Versions.Intersect(&remoteSetup.Versions)
	if err != nil {
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}

	if principal == nil {
		return nullCipher, nil, nil
	}

	// Perform the authentication.
	ver := vrange.Max
	remoteBox := remoteSetup.NaclBox()
	if remoteBox == nil {
		return nil, nil, verror.New(errNaclBoxVersionNegotiationFailed, nil)
	}
	remoteEP := remoteSetup.PeerEndpoint()

	var cipher crypto.ControlCipher
	switch {
	case ver < version.RPCVersion11:
		cipher = crypto.NewControlCipherRPC6(sk, &remoteBox.PublicKey, false)
	default:
		cipher = crypto.NewControlCipherRPC11(pk, sk, &remoteBox.PublicKey)
	}
	sconn := newSetupConn(writer, reader, cipher)
	crypter := crypto.NewNullCrypterWithChannelBinding(cipher.ChannelBinding())
	params := security.CallParams{LocalPrincipal: principal, LocalEndpoint: localEP}
	// TODO(jyh): act upon the authentication results.
	lBlessings, rBlessings, rDischarges, err := vc.AuthenticateAsClient(sconn, crypter, ver, params, auth)
	if err != nil {
		return nil, nil, err
	}
	if ver < version.RPCVersion11 || remoteEP == nil {
		// We do not return AuthenticationResult for old versions due to a channel binding bug.
		return cipher, nil, nil
	}

	authr := AuthenticationResult{
		Dialed:         true,
		Version:        ver,
		RemoteEndpoint: remoteEP,
		SessionKeys: SessionKeys{
			RemotePublic: remoteBox.PublicKey,
		},
		LocalBlessings:   lBlessings,
		RemoteBlessings:  rBlessings,
		RemoteDischarges: rDischarges,
	}
	return cipher, &authr, nil
}

// AuthenticateAsServer handles a Setup message, choosing authentication
// based on the max common version.
//
// See AuthenticateAsClient for a description of the negotiation.
func AuthenticateAsServer(writer io.Writer, reader *iobuf.Reader, localEP naming.Endpoint, versions *iversion.Range, principal security.Principal, lBlessings security.Blessings, dc vc.DischargeClient) (crypto.ControlCipher, *AuthenticationResult, error) {
	if versions == nil {
		versions = iversion.SupportedRange
	}

	// Send server's public data.
	setup, pk, sk, err := makeSetup(versions, localEP)
	if err != nil {
		return nil, nil, err
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
		errch <- message.WriteTo(writer, setup, nullCipher)
	}()

	// Read client's public data.
	remoteMsg, err := message.ReadFrom(reader, nullCipher)
	close(readch) // Note: we need to close this whether we get an error or not.
	if err != nil {
		<-errch
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}
	remoteSetup, ok := remoteMsg.(*message.Setup)
	if !ok {
		<-errch
		return nil, nil, verror.New(stream.ErrSecurity, nil, verror.New(errVersionNegotiationFailed, nil))
	}

	// Wait for the write to succeed.
	if err := <-errch; err != nil {
		return nil, nil, err
	}

	// Choose the max version in the intersection.
	vrange, err := versions.Intersect(&remoteSetup.Versions)
	if err != nil {
		return nil, nil, verror.New(stream.ErrNetwork, nil, err)
	}

	if principal == nil {
		return nullCipher, nil, nil
	}

	// Perform authentication.
	ver := vrange.Max
	remoteBox := remoteSetup.NaclBox()
	if remoteBox == nil {
		return nil, nil, verror.New(errNaclBoxVersionNegotiationFailed, nil)
	}
	remoteEP := remoteSetup.PeerEndpoint()

	var cipher crypto.ControlCipher
	switch {
	case ver < version.RPCVersion11:
		cipher = crypto.NewControlCipherRPC6(sk, &remoteBox.PublicKey, true)
	default:
		cipher = crypto.NewControlCipherRPC11(pk, sk, &remoteBox.PublicKey)
	}
	sconn := newSetupConn(writer, reader, cipher)
	crypter := crypto.NewNullCrypterWithChannelBinding(cipher.ChannelBinding())
	// TODO(jyh): act upon authentication results.
	rBlessings, lDischarges, err := vc.AuthenticateAsServer(sconn, crypter, ver, principal, lBlessings, dc)
	if err != nil {
		return nil, nil, verror.New(errAuthFailed, nil, err)
	}
	if ver < version.RPCVersion11 || remoteEP == nil {
		// We do not return AuthenticationResult for old versions due to a channel binding bug.
		return cipher, nil, nil
	}

	authr := AuthenticationResult{
		Version:        ver,
		RemoteEndpoint: remoteEP,
		SessionKeys: SessionKeys{
			LocalPublic:  *pk,
			LocalPrivate: *sk,
		},
		LocalBlessings:  lBlessings,
		RemoteBlessings: rBlessings,
		LocalDischarges: lDischarges,
	}
	return cipher, &authr, nil
}

// makeSetup constructs the options that this process can support.
func makeSetup(versions *iversion.Range, localEP naming.Endpoint) (setup *message.Setup, publicKey, privateKey *crypto.BoxKey, err error) {
	publicKey, privateKey, err = crypto.GenerateBoxKey()
	if err != nil {
		return nil, nil, nil, err
	}
	options := []message.SetupOption{&message.NaclBox{PublicKey: *publicKey}}
	if localEP != nil {
		options = append(options, &message.PeerEndpoint{LocalEndpoint: localEP})
	}
	setup = &message.Setup{Versions: *versions, Options: options}
	return
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
