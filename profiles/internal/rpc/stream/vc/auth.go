// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vc

import (
	"bytes"
	"io"

	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"

	"v.io/x/ref/profiles/internal/lib/iobuf"
	"v.io/x/ref/profiles/internal/rpc/stream"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
)

var (
	authServerContextTag = []byte("VCauthS\x00")
	authClientContextTag = []byte("VCauthC\x00")
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errVomDecoder                   = reg(".errVomDecoder", "failed to create vom decoder{:3}")
	errVomEncoder                   = reg(".errVomEncoder", "failed to create vom encoder{:3}")
	errVomEncodeBlessing            = reg(".errVomEncodeRequest", "failed to encode blessing{:3}")
	errHandshakeMessage             = reg(".errHandshakeMessage", "failed to read hanshake message{:3}")
	errInvalidSignatureInMessage    = reg(".errInvalidSignatureInMessage", "signature does not verify in authentication handshake message")
	errFailedToCreateSelfBlessing   = reg(".errFailedToCreateSelfBlessing", "failed to create self blessing{:3}")
	errNoBlessingsToPresentToServer = reg(".errerrNoBlessingsToPresentToServer ", "no blessings to present as a server")
)

// AuthenticateAsServer executes the authentication protocol at the server.
// It returns the blessings shared by the client, and the discharges shared
// by the server.
func AuthenticateAsServer(conn io.ReadWriteCloser, principal security.Principal, server security.Blessings, dc DischargeClient, crypter crypto.Crypter, v version.RPCVersion) (security.Blessings, map[string]security.Discharge, error) {
	if server.IsZero() {
		return security.Blessings{}, nil, verror.New(stream.ErrSecurity, nil, verror.New(errNoBlessingsToPresentToServer, nil))
	}
	var serverDischarges []security.Discharge
	if tpcavs := server.ThirdPartyCaveats(); len(tpcavs) > 0 && dc != nil {
		serverDischarges = dc.PrepareDischarges(nil, tpcavs, security.DischargeImpetus{})
	}
	if err := writeBlessings(conn, authServerContextTag, crypter, principal, server, serverDischarges, v); err != nil {
		return security.Blessings{}, nil, err
	}
	// Note that since the client uses a self-signed blessing to authenticate
	// during VC setup, it does not share any discharges.
	client, _, err := readBlessings(conn, authClientContextTag, crypter, v)
	if err != nil {
		return security.Blessings{}, nil, err
	}
	return client, mkDischargeMap(serverDischarges), nil
}

// AuthenticateAsClient executes the authentication protocol at the client.
// It returns the blessing shared by the server, the blessings shared by the
// client, and any discharges shared by the server.
//
// The client will only share its blessings if the server (who shares its
// blessings first) is authorized as per the authorizer for this RPC.
func AuthenticateAsClient(conn io.ReadWriteCloser, crypter crypto.Crypter, params security.CallParams, auth *ServerAuthorizer, v version.RPCVersion) (security.Blessings, security.Blessings, map[string]security.Discharge, error) {
	server, serverDischarges, err := readBlessings(conn, authServerContextTag, crypter, v)
	if err != nil {
		return security.Blessings{}, security.Blessings{}, nil, err
	}
	// Authorize the server based on the provided authorizer.
	if auth != nil {
		params.RemoteBlessings = server
		params.RemoteDischarges = serverDischarges
		if err := auth.Authorize(params); err != nil {
			return security.Blessings{}, security.Blessings{}, nil, verror.New(stream.ErrNotTrusted, nil, err)
		}
	}

	// The client shares its blessings at RPC time (as the blessings may vary
	// across RPCs). During VC handshake, the client simply sends a self-signed
	// blessing in order to reveal its public key to the server.
	principal := params.LocalPrincipal
	client, err := principal.BlessSelf("vcauth")
	if err != nil {
		return security.Blessings{}, security.Blessings{}, nil, verror.New(stream.ErrSecurity, nil, verror.New(errFailedToCreateSelfBlessing, nil, err))
	}
	if err := writeBlessings(conn, authClientContextTag, crypter, principal, client, nil, v); err != nil {
		return security.Blessings{}, security.Blessings{}, nil, err
	}
	return server, client, serverDischarges, nil
}

func writeBlessings(w io.Writer, tag []byte, crypter crypto.Crypter, p security.Principal, b security.Blessings, discharges []security.Discharge, v version.RPCVersion) error {
	signature, err := p.Sign(append(tag, crypter.ChannelBinding()...))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc, err := vom.NewEncoder(&buf)
	if err != nil {
		return verror.New(stream.ErrNetwork, nil, verror.New(errVomEncoder, nil, err))
	}
	if err := enc.Encode(signature); err != nil {
		return verror.New(stream.ErrNetwork, nil, verror.New(errVomEncodeBlessing, nil, err))
	}
	if err := enc.Encode(b); err != nil {
		return verror.New(stream.ErrNetwork, nil, verror.New(errVomEncodeBlessing, nil, err))
	}
	if v >= version.RPCVersion5 {
		if err := enc.Encode(discharges); err != nil {
			return verror.New(stream.ErrNetwork, nil, verror.New(errVomEncodeBlessing, nil, err))
		}
	}
	msg, err := crypter.Encrypt(iobuf.NewSlice(buf.Bytes()))
	if err != nil {
		return err
	}
	defer msg.Release()
	enc, err = vom.NewEncoder(w)
	if err != nil {
		return verror.New(stream.ErrNetwork, nil, verror.New(errVomEncoder, nil, err))
	}
	if err := enc.Encode(msg.Contents); err != nil {
		return verror.New(stream.ErrNetwork, nil, verror.New(errVomEncodeBlessing, nil, err))
	}
	return nil
}

func readBlessings(r io.Reader, tag []byte, crypter crypto.Crypter, v version.RPCVersion) (security.Blessings, map[string]security.Discharge, error) {
	var msg []byte
	var noBlessings security.Blessings
	dec, err := vom.NewDecoder(r)
	if err != nil {
		return noBlessings, nil, verror.New(stream.ErrNetwork, nil, verror.New(errVomDecoder, nil, err))
	}
	if err := dec.Decode(&msg); err != nil {
		return noBlessings, nil, verror.New(stream.ErrNetwork, nil, verror.New(errHandshakeMessage, nil, err))
	}
	buf, err := crypter.Decrypt(iobuf.NewSlice(msg))
	if err != nil {
		return noBlessings, nil, err
	}
	defer buf.Release()
	dec, err = vom.NewDecoder(bytes.NewReader(buf.Contents))
	if err != nil {
		return noBlessings, nil, verror.New(stream.ErrNetwork, nil, verror.New(errVomDecoder, nil, err))
	}

	var (
		blessings security.Blessings
		sig       security.Signature
	)
	if err = dec.Decode(&sig); err != nil {
		return noBlessings, nil, verror.New(stream.ErrNetwork, nil, err)
	}
	if err = dec.Decode(&blessings); err != nil {
		return noBlessings, nil, verror.New(stream.ErrNetwork, nil, err)
	}
	var discharges []security.Discharge
	if v >= version.RPCVersion5 {
		if err := dec.Decode(&discharges); err != nil {
			return noBlessings, nil, verror.New(stream.ErrNetwork, nil, err)
		}
	}
	if !sig.Verify(blessings.PublicKey(), append(tag, crypter.ChannelBinding()...)) {
		return noBlessings, nil, verror.New(stream.ErrSecurity, nil, verror.New(errInvalidSignatureInMessage, nil))
	}
	return blessings, mkDischargeMap(discharges), nil
}

func mkDischargeMap(discharges []security.Discharge) map[string]security.Discharge {
	if len(discharges) == 0 {
		return nil
	}
	m := make(map[string]security.Discharge, len(discharges))
	for _, d := range discharges {
		m[d.ID()] = d
	}
	return m
}
