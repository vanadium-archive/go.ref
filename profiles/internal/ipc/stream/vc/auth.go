package vc

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"v.io/x/ref/profiles/internal/ipc/stream/crypto"
	"v.io/x/ref/profiles/internal/lib/iobuf"

	"v.io/v23/ipc/version"
	"v.io/v23/security"
	"v.io/v23/vom"
)

var (
	authServerContextTag = []byte("VCauthS\x00")
	authClientContextTag = []byte("VCauthC\x00")
)

var (
	errSameChannelPublicKey      = errors.New("same public keys for both ends of the channel")
	errChannelIDMismatch         = errors.New("channel id does not match expectation")
	errChecksumMismatch          = errors.New("checksum mismatch")
	errInvalidSignatureInMessage = errors.New("signature does not verify in authentication handshake message")
	errNoCertificatesReceived    = errors.New("no certificates received")
	errSingleCertificateRequired = errors.New("exactly one X.509 certificate chain with exactly one certificate is required")
)

// AuthenticateAsServer executes the authentication protocol at the server and
// returns the blessings used to authenticate the client.
func AuthenticateAsServer(conn io.ReadWriteCloser, principal security.Principal, server security.Blessings, dc DischargeClient, crypter crypto.Crypter, v version.IPCVersion) (client security.Blessings, err error) {
	if server.IsZero() {
		return security.Blessings{}, errors.New("no blessings to present as a server")
	}
	var discharges []security.Discharge
	if tpcavs := server.ThirdPartyCaveats(); len(tpcavs) > 0 && dc != nil {
		discharges = dc.PrepareDischarges(nil, tpcavs, security.DischargeImpetus{})
	}
	if err = writeBlessings(conn, authServerContextTag, crypter, principal, server, discharges, v); err != nil {
		return
	}
	if client, _, err = readBlessings(conn, authClientContextTag, crypter, v); err != nil {
		return
	}
	return
}

// AuthenticateAsClient executes the authentication protocol at the client and
// returns the blessings used to authenticate both ends.
//
// The client will only share its blessings if the server (who shares its blessings first)
// is authorized as per the authorizer for this RPC.
func AuthenticateAsClient(conn io.ReadWriteCloser, crypter crypto.Crypter, params security.CallParams, auth *ServerAuthorizer, v version.IPCVersion) (server, client security.Blessings, serverDischarges map[string]security.Discharge, err error) {
	if server, serverDischarges, err = readBlessings(conn, authServerContextTag, crypter, v); err != nil {
		return
	}
	// Authorize the server based on the provided authorizer.
	if auth != nil {
		params.RemoteBlessings = server
		params.RemoteDischarges = serverDischarges
		if err = auth.Authorize(params); err != nil {
			return
		}
	}

	// The client shares its blessings at RPC time (as the blessings may vary across
	// RPCs). During VC handshake, the client simply sends a self-signed blessing
	// in order to reveal its public key to the server.
	principal := params.LocalPrincipal
	client, err = principal.BlessSelf("vcauth")
	if err != nil {
		return security.Blessings{}, security.Blessings{}, nil, fmt.Errorf("failed to created self blessing: %v", err)
	}
	if err = writeBlessings(conn, authClientContextTag, crypter, principal, client, nil, v); err != nil {
		return
	}
	return
}

func writeBlessings(w io.Writer, tag []byte, crypter crypto.Crypter, p security.Principal, b security.Blessings, discharges []security.Discharge, v version.IPCVersion) error {
	signature, err := p.Sign(append(tag, crypter.ChannelBinding()...))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc, err := vom.NewEncoder(&buf)
	if err != nil {
		return err
	}
	if err := enc.Encode(signature); err != nil {
		return err
	}
	if err := enc.Encode(b); err != nil {
		return err
	}
	if v >= version.IPCVersion5 {
		if err := enc.Encode(discharges); err != nil {
			return err
		}
	}
	msg, err := crypter.Encrypt(iobuf.NewSlice(buf.Bytes()))
	if err != nil {
		return err
	}
	defer msg.Release()
	enc, err = vom.NewEncoder(w)
	if err != nil {
		return err
	}
	return enc.Encode(msg.Contents)
}

func readBlessings(r io.Reader, tag []byte, crypter crypto.Crypter, v version.IPCVersion) (security.Blessings, map[string]security.Discharge, error) {
	var msg []byte
	var noBlessings security.Blessings
	dec, err := vom.NewDecoder(r)
	if err != nil {
		return noBlessings, nil, fmt.Errorf("failed to create new decoder: %v", err)
	}
	if err := dec.Decode(&msg); err != nil {
		return noBlessings, nil, fmt.Errorf("failed to read handshake message: %v", err)
	}
	buf, err := crypter.Decrypt(iobuf.NewSlice(msg))
	if err != nil {
		return noBlessings, nil, err
	}
	defer buf.Release()
	dec, err = vom.NewDecoder(bytes.NewReader(buf.Contents))
	if err != nil {
		return noBlessings, nil, fmt.Errorf("failed to create new decoder: %v", err)
	}

	var (
		blessings security.Blessings
		sig       security.Signature
	)
	if err = dec.Decode(&sig); err != nil {
		return noBlessings, nil, err
	}
	if err = dec.Decode(&blessings); err != nil {
		return noBlessings, nil, err
	}
	var discharges map[string]security.Discharge
	if v >= version.IPCVersion5 {
		var list []security.Discharge
		if err := dec.Decode(&list); err != nil {
			return noBlessings, nil, err
		}
		if len(list) > 0 {
			discharges = make(map[string]security.Discharge)
			for _, d := range list {
				discharges[d.ID()] = d
			}
		}
	}
	if !sig.Verify(blessings.PublicKey(), append(tag, crypter.ChannelBinding()...)) {
		return noBlessings, nil, errInvalidSignatureInMessage
	}
	return blessings, discharges, nil
}
