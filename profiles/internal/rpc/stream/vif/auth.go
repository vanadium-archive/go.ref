package vif

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"

	"v.io/v23/options"
	rpcversion "v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/x/ref/profiles/internal/lib/iobuf"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	"v.io/x/ref/profiles/internal/rpc/stream/message"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
	"v.io/x/ref/profiles/internal/rpc/version"
)

var (
	errUnsupportedEncryptVersion = errors.New("unsupported encryption version")
	errVersionNegotiationFailed  = errors.New("encryption version negotiation failed")
	nullCipher                   crypto.NullControlCipher
)

// privateData includes secret data we need for encryption.
type privateData struct {
	naclBoxPrivateKey [32]byte
}

// AuthenticateAsClient sends a HopSetup message if possible.  If so, it chooses
// encryption based on the max supported version.
//
// The sequence is initiated by the client.
//
//    - If the versions include RPCVersion6 or greater, the client sends a
//      HopSetup message to the server, containing the client's supported
//      versions, and the client's crypto options.  The HopSetup message
//      is sent in the clear.
//
//    - When the server receives the HopSetup message, it calls
//      AuthenticateAsServer, which constructs a response HopSetup containing
//      the server's version range, and any crypto options.
//
//    - For RPCVersion6 and RPCVersion7, the client and server generate fresh
//      public/private key pairs, sending the public key to the peer as a crypto
//      option.  The remainder of the communication is encrypted as
//      HopSetupStream messages using NewControlCipherRPC6, which is based on
//      code.google.com/p/go.crypto/nacl/box.
//
//    - Once the encrypted HopSetupStream channel is setup, the client and
//      server authenticate using the vc.AuthenticateAs{Client,Server} protocol.
//
// Note that the HopSetup messages are sent in the clear, so they are subject to
// modification by a man-in-the-middle, which can currently force a downgrade by
// modifying the acceptable version ranges downward.  This can be addressed by
// including a hash of the HopSetup message in the encrypted stream.  It is
// likely that this will be addressed in subsequent protocol versions (or it may
// not be addressed at all if RPCVersion6 becomes the only supported version).
func AuthenticateAsClient(writer io.Writer, reader *iobuf.Reader, versions *version.Range, params security.CallParams, auth *vc.ServerAuthorizer) (crypto.ControlCipher, error) {
	if versions == nil {
		versions = version.SupportedRange
	}
	if params.LocalPrincipal == nil {
		// If there is no principal, we do not support encryption/authentication.
		var err error
		versions, err = versions.Intersect(&version.Range{Min: 0, Max: rpcversion.RPCVersion5})
		if err != nil {
			return nil, err
		}
	}
	if versions.Max < rpcversion.RPCVersion6 {
		return nullCipher, nil
	}

	// The client has not yet sent its public data.  Construct it and send it.
	pvt, pub, err := makeHopSetup(versions)
	if err != nil {
		return nil, err
	}
	if err := message.WriteTo(writer, &pub, nullCipher); err != nil {
		return nil, err
	}

	// Read the server's public data.
	pmsg, err := message.ReadFrom(reader, nullCipher)
	if err != nil {
		return nil, err
	}
	ppub, ok := pmsg.(*message.HopSetup)
	if !ok {
		return nil, errVersionNegotiationFailed
	}

	// Choose the max version in the intersection.
	vrange, err := pub.Versions.Intersect(&ppub.Versions)
	if err != nil {
		return nil, err
	}
	v := vrange.Max
	if v < rpcversion.RPCVersion6 {
		return nullCipher, nil
	}

	// Perform the authentication.
	return authenticateAsClient(writer, reader, params, auth, &pvt, &pub, ppub, v)
}

func authenticateAsClient(writer io.Writer, reader *iobuf.Reader, params security.CallParams, auth *vc.ServerAuthorizer,
	pvt *privateData, pub, ppub *message.HopSetup, version rpcversion.RPCVersion) (crypto.ControlCipher, error) {
	if version < rpcversion.RPCVersion6 {
		return nil, errUnsupportedEncryptVersion
	}
	pbox := ppub.NaclBox()
	if pbox == nil {
		return nil, errVersionNegotiationFailed
	}
	c := crypto.NewControlCipherRPC6(&pbox.PublicKey, &pvt.naclBoxPrivateKey, false)
	sconn := newSetupConn(writer, reader, c)
	// TODO(jyh): act upon the authentication results.
	_, _, _, err := vc.AuthenticateAsClient(sconn, crypto.NewNullCrypter(), params, auth, version)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %v", err)
	}
	return c, nil
}

// AuthenticateAsServer handles a HopSetup message, choosing authentication
// based on the max common version.
//
// See AuthenticateAsClient for a description of the negotiation.
func AuthenticateAsServer(writer io.Writer, reader *iobuf.Reader, versions *version.Range, principal security.Principal, lBlessings security.Blessings,
	dc vc.DischargeClient, ppub *message.HopSetup) (crypto.ControlCipher, error) {
	var err error
	if versions == nil {
		versions = version.SupportedRange
	}
	if principal == nil {
		// If there is no principal, we don't support encryption/authentication.
		versions, err = versions.Intersect(&version.Range{Min: 0, Max: rpcversion.RPCVersion5})
		if err != nil {
			return nil, err
		}
	}

	// Create our public data and send it to the client.
	pvt, pub, err := makeHopSetup(versions)
	if err != nil {
		return nil, err
	}
	if err := message.WriteTo(writer, &pub, nullCipher); err != nil {
		return nil, err
	}

	// Choose the max version in common.
	vrange, err := pub.Versions.Intersect(&ppub.Versions)
	if err != nil {
		return nil, err
	}
	v := vrange.Max
	if v < rpcversion.RPCVersion6 {
		return nullCipher, nil
	}

	// Perform authentication.
	return authenticateAsServerRPC6(writer, reader, principal, lBlessings, dc, &pvt, &pub, ppub, v)
}

func authenticateAsServerRPC6(writer io.Writer, reader *iobuf.Reader, principal security.Principal, lBlessings security.Blessings, dc vc.DischargeClient,
	pvt *privateData, pub, ppub *message.HopSetup, version rpcversion.RPCVersion) (crypto.ControlCipher, error) {
	if version < rpcversion.RPCVersion6 {
		return nil, errUnsupportedEncryptVersion
	}
	box := ppub.NaclBox()
	if box == nil {
		return nil, errVersionNegotiationFailed
	}
	c := crypto.NewControlCipherRPC6(&box.PublicKey, &pvt.naclBoxPrivateKey, true)
	sconn := newSetupConn(writer, reader, c)
	// TODO(jyh): act upon authentication results.
	_, _, err := vc.AuthenticateAsServer(sconn, principal, lBlessings, dc, crypto.NewNullCrypter(), version)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %v", err)
	}
	return c, nil
}

// serverAuthOptions returns credentials for VIF authentication, based on the provided principal and options list.
func serverAuthOptions(principal security.Principal, lopts []stream.ListenerOpt) (security.Principal, security.Blessings, vc.DischargeClient, error) {
	var (
		securityLevel   options.VCSecurityLevel
		dischargeClient vc.DischargeClient
		lBlessings      security.Blessings
	)
	for _, o := range lopts {
		switch v := o.(type) {
		case vc.DischargeClient:
			dischargeClient = v
		case options.VCSecurityLevel:
			securityLevel = v
		case options.ServerBlessings:
			lBlessings = v.Blessings
		}
	}
	switch securityLevel {
	case options.VCSecurityConfidential:
		if lBlessings.IsZero() {
			lBlessings = principal.BlessingStore().Default()
		}
		return principal, lBlessings, dischargeClient, nil
	case options.VCSecurityNone:
		return nil, security.Blessings{}, nil, nil
	default:
		return nil, security.Blessings{}, nil, fmt.Errorf("unrecognized VC security level: %v", securityLevel)
	}
}

// makeHopSetup constructs the options that this process can support.
func makeHopSetup(versions *version.Range) (pvt privateData, pub message.HopSetup, err error) {
	pub.Versions = *versions
	var pubKey, pvtKey *[32]byte
	pubKey, pvtKey, err = box.GenerateKey(rand.Reader)
	pub.Options = append(pub.Options, &message.NaclBox{PublicKey: *pubKey})
	pvt.naclBoxPrivateKey = *pvtKey
	return
}
