// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"crypto/rand"
	"io"
	"reflect"
	"sync"
	"time"

	"golang.org/x/crypto/nacl/box"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"
	slib "v.io/x/ref/lib/security"
	iflow "v.io/x/ref/runtime/internal/flow"
	inaming "v.io/x/ref/runtime/internal/naming"
)

var (
	authDialerTag   = []byte("AuthDial\x00")
	authAcceptorTag = []byte("AuthAcpt\x00")
)

func (c *Conn) dialHandshake(ctx *context.T, versions version.RPCVersionRange, auth flow.PeerAuthorizer) error {
	binding, remoteEndpoint, err := c.setup(ctx, versions, true)
	if err != nil {
		return err
	}
	isProxy := c.remote.RoutingID() != naming.NullRoutingID && c.remote.RoutingID() != remoteEndpoint.RoutingID()
	// We use the remote ends local endpoint as our remote endpoint when the routingID's
	// of the endpoints differ. This is an indicator that we are talking to a proxy.
	// This means that the manager will need to dial a subsequent conn on this conn
	// to the end server.
	c.remote.(*inaming.Endpoint).RID = remoteEndpoint.RoutingID()
	bflow := c.newFlowLocked(ctx, blessingsFlowID, 0, 0, nil, true, true, 0)
	bflow.releaseLocked(DefaultBytesBufferedPerFlow)
	c.blessingsFlow = newBlessingsFlow(ctx, &c.loopWG, bflow)

	rBlessings, rDischarges, err := c.readRemoteAuth(ctx, binding, true)
	if err != nil {
		return err
	}
	if rBlessings.IsZero() {
		return NewErrAcceptorBlessingsMissing(ctx)
	}
	if !isProxy {
		if _, _, err := auth.AuthorizePeer(ctx, c.local, c.remote, rBlessings, rDischarges); err != nil {
			return iflow.MaybeWrapError(verror.ErrNotTrusted, ctx, err)
		}
	}
	signedBinding, err := v23.GetPrincipal(ctx).Sign(append(authDialerTag, binding...))
	if err != nil {
		return err
	}
	lAuth := &message.Auth{
		ChannelBinding: signedBinding,
	}
	// We only send our real blessings if we are a server in addition to being a client.
	// Otherwise, we only send our public key through a nameless blessings object.
	// TODO(suharshs): Should we reveal server blessings if we are connecting to proxy here.
	if c.lBlessings.IsZero() || c.handler == nil {
		c.lBlessings, _ = security.NamelessBlessing(v23.GetPrincipal(ctx).PublicKey())
	}
	// The client sends its blessings without any blessing-pattern encryption to the
	// server as it has already authorized the server. Thus the 'peers' argument to
	// blessingsFlow.send is nil.
	if lAuth.BlessingsKey, _, err = c.blessingsFlow.send(ctx, c.lBlessings, nil, nil); err != nil {
		return err
	}
	defer c.mu.Unlock()
	c.mu.Lock()
	return c.sendMessageLocked(ctx, true, expressPriority, lAuth)
}

func (c *Conn) acceptHandshake(ctx *context.T, versions version.RPCVersionRange, authorizedPeers []security.BlessingPattern) error {
	binding, remoteEndpoint, err := c.setup(ctx, versions, false)
	if err != nil {
		return err
	}
	c.remote = remoteEndpoint
	c.blessingsFlow = newBlessingsFlow(ctx, &c.loopWG,
		c.newFlowLocked(ctx, blessingsFlowID, 0, 0, nil, true, true, 0))
	signedBinding, err := v23.GetPrincipal(ctx).Sign(append(authAcceptorTag, binding...))
	if err != nil {
		return err
	}
	lAuth := &message.Auth{
		ChannelBinding: signedBinding,
	}
	if lAuth.BlessingsKey, lAuth.DischargeKey, err = c.refreshDischarges(ctx, false, authorizedPeers); err != nil {
		return err
	}
	c.mu.Lock()
	err = c.sendMessageLocked(ctx, true, expressPriority, lAuth)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	_, _, err = c.readRemoteAuth(ctx, binding, false)
	return err
}

func (c *Conn) setup(ctx *context.T, versions version.RPCVersionRange, dialer bool) ([]byte, naming.Endpoint, error) {
	pk, sk, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	lSetup := &message.Setup{
		Versions:          versions,
		PeerLocalEndpoint: c.local,
		PeerNaClPublicKey: pk,
		Mtu:               defaultMtu,
		SharedTokens:      DefaultBytesBufferedPerFlow,
	}
	if c.remote != nil {
		lSetup.PeerRemoteEndpoint = c.remote
	}
	ch := make(chan error, 1)
	go func() {
		c.mu.Lock()
		ch <- c.sendMessageLocked(ctx, true, expressPriority, lSetup)
		c.mu.Unlock()
	}()
	msg, err := c.mp.readMsg(ctx)
	if err != nil {
		<-ch
		return nil, nil, NewErrRecv(ctx, "unknown", err)
	}
	rSetup, valid := msg.(*message.Setup)
	if !valid {
		<-ch
		return nil, nil, NewErrUnexpectedMsg(ctx, reflect.TypeOf(msg).String())
	}
	if err := <-ch; err != nil {
		remoteStr := ""
		if c.remote != nil {
			remoteStr = c.remote.String()
		}
		return nil, nil, NewErrSend(ctx, "setup", remoteStr, err)
	}
	if c.version, err = version.CommonVersion(ctx, lSetup.Versions, rSetup.Versions); err != nil {
		return nil, nil, err
	}
	if c.local == nil {
		c.local = rSetup.PeerRemoteEndpoint
	}
	if rSetup.Mtu != 0 {
		c.mtu = rSetup.Mtu
	} else {
		c.mtu = defaultMtu
	}
	c.lshared = lSetup.SharedTokens
	if rSetup.SharedTokens != 0 && rSetup.SharedTokens < c.lshared {
		c.lshared = rSetup.SharedTokens
	}
	if rSetup.PeerNaClPublicKey == nil {
		return nil, nil, NewErrMissingSetupOption(ctx, "peerNaClPublicKey")
	}
	binding := c.mp.setupEncryption(ctx, pk, sk, rSetup.PeerNaClPublicKey)
	if c.version >= version.RPCVersion14 {
		// We include the setup messages in the channel binding to prevent attacks
		// where a man in the middle changes fields in the Setup message (e.g. a
		// downgrade attack wherein a MITM attacker changes the Version field of
		// the Setup message to a lower-security version.)
		// We always put the dialer first in the binding.
		if dialer {
			if binding, err = message.Append(ctx, lSetup, nil); err != nil {
				return nil, nil, err
			}
			if binding, err = message.Append(ctx, rSetup, binding); err != nil {
				return nil, nil, err
			}
		} else {
			if binding, err = message.Append(ctx, rSetup, nil); err != nil {
				return nil, nil, err
			}
			if binding, err = message.Append(ctx, lSetup, binding); err != nil {
				return nil, nil, err
			}
		}
	}
	// if we're encapsulated in another flow, tell that flow to stop
	// encrypting now that we've started.
	if f, ok := c.mp.rw.(*flw); ok {
		f.disableEncryption()
	}
	return binding, rSetup.PeerLocalEndpoint, nil
}

func (c *Conn) readRemoteAuth(ctx *context.T, binding []byte, dialer bool) (security.Blessings, map[string]security.Discharge, error) {
	tag := authDialerTag
	if dialer {
		tag = authAcceptorTag
	}
	var (
		rauth *message.Auth
		err   error
	)
	for {
		msg, err := c.mp.readMsg(ctx)
		if err != nil {
			return security.Blessings{}, nil, NewErrRecv(ctx, c.remote.String(), err)
		}
		if rauth, _ = msg.(*message.Auth); rauth != nil {
			break
		}
		if err = c.handleMessage(ctx, msg); err != nil {
			return security.Blessings{}, nil, err
		}
	}
	c.rBKey = rauth.BlessingsKey
	// Only read the blessings if we were the dialer. Any blessings from the dialer
	// will be sent later.
	var rBlessings security.Blessings
	var rDischarges map[string]security.Discharge
	if rauth.BlessingsKey != 0 {
		// TODO(mattr): Make sure we cancel out of this at some point.
		rBlessings, rDischarges, err = c.blessingsFlow.getRemote(ctx, rauth.BlessingsKey, rauth.DischargeKey)
		if err != nil {
			return security.Blessings{}, nil, err
		}
		c.mu.Lock()
		c.rPublicKey = rBlessings.PublicKey()
		c.mu.Unlock()
	}
	if c.rPublicKey == nil {
		return security.Blessings{}, nil, NewErrNoPublicKey(ctx)
	}
	if !rauth.ChannelBinding.Verify(c.rPublicKey, append(tag, binding...)) {
		return security.Blessings{}, nil, NewErrInvalidChannelBinding(ctx)
	}
	return rBlessings, rDischarges, nil
}

func (c *Conn) refreshDischarges(ctx *context.T, loop bool, peers []security.BlessingPattern) (bkey, dkey uint64, err error) {
	dis, refreshTime := slib.PrepareDischarges(ctx, c.lBlessings, security.DischargeImpetus{})
	// Schedule the next update.
	c.mu.Lock()
	if loop && !refreshTime.IsZero() && c.status < Closing {
		c.loopWG.Add(1)
		c.dischargeTimer = time.AfterFunc(refreshTime.Sub(time.Now()), func() {
			c.refreshDischarges(ctx, true, peers)
			c.loopWG.Done()
		})
	}
	c.mu.Unlock()
	bkey, dkey, err = c.blessingsFlow.send(ctx, c.lBlessings, dis, peers)
	return
}

func newBlessingsFlow(ctx *context.T, loopWG *sync.WaitGroup, f *flw) *blessingsFlow {
	b := &blessingsFlow{
		f:       f,
		enc:     vom.NewEncoder(f),
		dec:     vom.NewDecoder(f),
		nextKey: 1,
		incoming: &inCache{
			blessings:  make(map[uint64]security.Blessings),
			dkeys:      make(map[uint64]uint64),
			discharges: make(map[uint64][]security.Discharge),
		},
		outgoing: &outCache{
			bkeys:      make(map[string]uint64),
			dkeys:      make(map[uint64]uint64),
			blessings:  make(map[uint64]security.Blessings),
			discharges: make(map[uint64][]security.Discharge),
		},
	}
	b.cond = sync.NewCond(&b.mu)
	loopWG.Add(1)
	go b.readLoop(ctx, loopWG)
	return b
}

type blessingsFlow struct {
	enc *vom.Encoder
	dec *vom.Decoder
	f   *flw

	mu       sync.Mutex
	cond     *sync.Cond
	closeErr error
	nextKey  uint64
	incoming *inCache
	outgoing *outCache
}

// inCache keeps track of incoming blessings, discharges, and keys.
type inCache struct {
	dkeys      map[uint64]uint64               // bkey -> dkey of the latest discharges.
	blessings  map[uint64]security.Blessings   // keyed by bkey
	discharges map[uint64][]security.Discharge // keyed by dkey
}

// outCache keeps track of outgoing blessings, discharges, and keys.
type outCache struct {
	bkeys map[string]uint64 // blessings uid -> bkey

	dkeys      map[uint64]uint64               // blessings bkey -> dkey of latest discharges
	blessings  map[uint64]security.Blessings   // keyed by bkey
	discharges map[uint64][]security.Discharge // keyed by dkey
}

func (b *blessingsFlow) receiveBlessings(ctx *context.T, bkey uint64, blessings security.Blessings) error {
	// When accepting, make sure the blessings received are bound to the conn's
	// remote public key.
	b.f.conn.mu.Lock()
	if pk := b.f.conn.rPublicKey; pk != nil && !reflect.DeepEqual(blessings.PublicKey(), pk) {
		b.f.conn.mu.Unlock()
		return NewErrBlessingsNotBound(ctx)
	}
	b.f.conn.mu.Unlock()
	b.mu.Lock()
	b.incoming.blessings[bkey] = blessings
	b.mu.Unlock()
	return nil
}

func (b *blessingsFlow) receiveDischarges(ctx *context.T, bkey, dkey uint64, discharges []security.Discharge) {
	b.mu.Lock()
	b.incoming.discharges[dkey] = discharges
	b.incoming.dkeys[bkey] = dkey
	b.mu.Unlock()
}

func (b *blessingsFlow) receive(ctx *context.T, bd BlessingsFlowMessage) error {
	switch bd := bd.(type) {
	case BlessingsFlowMessageBlessings:
		bkey, blessings := bd.Value.BKey, bd.Value.Blessings
		if err := b.receiveBlessings(ctx, bkey, blessings); err != nil {
			return err
		}
	case BlessingsFlowMessageEncryptedBlessings:
		bkey, ciphertexts := bd.Value.BKey, bd.Value.Ciphertexts
		var blessings security.Blessings
		if err := decrypt(ctx, ciphertexts, &blessings); err != nil {
			// TODO(ataly): This error should not be returned if the
			// client has explicitly set the peer authorizer to nil.
			// In that case, the client does not care whether the server's
			// blessings can be decrypted or not. Ideally we should just
			// pass this error to the peer authorizer and handle it there.
			return iflow.MaybeWrapError(verror.ErrNotTrusted, ctx, NewErrCannotDecryptBlessings(ctx, err))
		}
		if err := b.receiveBlessings(ctx, bkey, blessings); err != nil {
			return err
		}
	case BlessingsFlowMessageDischarges:
		bkey, dkey, discharges := bd.Value.BKey, bd.Value.DKey, bd.Value.Discharges
		b.receiveDischarges(ctx, bkey, dkey, discharges)
	case BlessingsFlowMessageEncryptedDischarges:
		bkey, dkey, ciphertexts := bd.Value.BKey, bd.Value.DKey, bd.Value.Ciphertexts
		var discharges []security.Discharge
		if err := decrypt(ctx, ciphertexts, &discharges); err != nil {
			return iflow.MaybeWrapError(verror.ErrNotTrusted, ctx, NewErrCannotDecryptDischarges(ctx, err))
		}
		b.receiveDischarges(ctx, bkey, dkey, discharges)
	}
	b.cond.Broadcast()
	return nil
}

func (b *blessingsFlow) getRemote(ctx *context.T, bkey, dkey uint64) (security.Blessings, map[string]security.Discharge, error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	for {
		blessings, hasB := b.incoming.blessings[bkey]
		if hasB {
			if dkey == 0 {
				return blessings, nil, nil
			}
			discharges, hasD := b.incoming.discharges[dkey]
			if hasD {
				return blessings, dischargeMap(discharges), nil
			}
		}
		// We check closeErr after we check the map to allow gets to succeed even after
		// the blessings flow is closed.
		if b.closeErr != nil {
			break
		}
		b.cond.Wait()
	}
	return security.Blessings{}, nil, b.closeErr
}

func (b *blessingsFlow) getLatestRemote(ctx *context.T, bkey uint64) (security.Blessings, map[string]security.Discharge, error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	for {
		blessings, has := b.incoming.blessings[bkey]
		if has {
			dkey := b.incoming.dkeys[bkey]
			discharges := b.incoming.discharges[dkey]
			return blessings, dischargeMap(discharges), nil
		}
		// We check closeErr after we check the map to allow gets to succeed even after
		// the blessings flow is closed.
		if b.closeErr != nil {
			break
		}
		b.cond.Wait()
	}
	return security.Blessings{}, nil, b.closeErr
}

func (b *blessingsFlow) encodeBlessingsLocked(ctx *context.T, blessings security.Blessings, bkey uint64, peers []security.BlessingPattern) error {
	if len(peers) == 0 {
		// blessings can be encoded in plaintext
		return b.enc.Encode(BlessingsFlowMessageBlessings{Blessings{
			BKey:      bkey,
			Blessings: blessings,
		}})
	}
	ciphertexts, err := encrypt(ctx, peers, blessings)
	if err != nil {
		return NewErrCannotEncryptBlessings(ctx, peers, err)
	}
	return b.enc.Encode(BlessingsFlowMessageEncryptedBlessings{EncryptedBlessings{
		BKey:        bkey,
		Ciphertexts: ciphertexts,
	}})
}

func (b *blessingsFlow) encodeDischargesLocked(ctx *context.T, discharges []security.Discharge, bkey, dkey uint64, peers []security.BlessingPattern) error {
	if len(peers) == 0 {
		// discharges can be encoded in plaintext
		return b.enc.Encode(BlessingsFlowMessageDischarges{Discharges{
			Discharges: discharges,
			DKey:       dkey,
			BKey:       bkey,
		}})
	}
	ciphertexts, err := encrypt(ctx, peers, discharges)
	if err != nil {
		return NewErrCannotEncryptDischarges(ctx, peers, err)
	}
	return b.enc.Encode(BlessingsFlowMessageEncryptedDischarges{EncryptedDischarges{
		DKey:        dkey,
		BKey:        bkey,
		Ciphertexts: ciphertexts,
	}})
}

func (b *blessingsFlow) send(ctx *context.T, blessings security.Blessings, discharges map[string]security.Discharge, peers []security.BlessingPattern) (bkey, dkey uint64, err error) {
	if blessings.IsZero() {
		return 0, 0, nil
	}
	defer b.mu.Unlock()
	b.mu.Lock()
	buid := string(blessings.UniqueID())
	bkey, hasB := b.outgoing.bkeys[buid]
	if !hasB {
		bkey = b.nextKey
		b.nextKey++
		b.outgoing.bkeys[buid] = bkey
		b.outgoing.blessings[bkey] = blessings
		if err := b.encodeBlessingsLocked(ctx, blessings, bkey, peers); err != nil {
			return 0, 0, err
		}
	}
	if len(discharges) == 0 {
		return bkey, 0, nil
	}
	dkey, hasD := b.outgoing.dkeys[bkey]
	if hasD && equalDischarges(discharges, b.outgoing.discharges[dkey]) {
		return bkey, dkey, nil
	}
	dlist := dischargeList(discharges)
	dkey = b.nextKey
	b.nextKey++
	b.outgoing.dkeys[bkey] = dkey
	b.outgoing.discharges[dkey] = dlist
	return bkey, dkey, b.encodeDischargesLocked(ctx, dlist, bkey, dkey, peers)
}

func (b *blessingsFlow) getLocal(ctx *context.T, bkey, dkey uint64) (security.Blessings, map[string]security.Discharge) {
	defer b.mu.Unlock()
	b.mu.Lock()
	blessings := b.outgoing.blessings[bkey]
	discharges := b.outgoing.discharges[dkey]
	return blessings, dischargeMap(discharges)
}

func (b *blessingsFlow) getLatestLocal(ctx *context.T, blessings security.Blessings) map[string]security.Discharge {
	defer b.mu.Unlock()
	b.mu.Lock()
	buid := string(blessings.UniqueID())
	bkey := b.outgoing.bkeys[buid]
	dkey := b.outgoing.dkeys[bkey]
	discharges := b.outgoing.discharges[dkey]
	return dischargeMap(discharges)
}

func (b *blessingsFlow) readLoop(ctx *context.T, loopWG *sync.WaitGroup) {
	defer loopWG.Done()
	for {
		var received BlessingsFlowMessage
		err := b.dec.Decode(&received)
		if err != nil {
			if err != io.EOF {
				// TODO(mattr): In practice this is very spammy,
				// figure out how to log it more effectively.
				ctx.VI(3).Infof("Blessings flow closed: %v", err)
			}
			b.mu.Lock()
			b.closeErr = NewErrBlessingsFlowClosed(ctx, err)
			b.mu.Unlock()
			return
		}
		if err := b.receive(ctx, received); err != nil {
			b.f.conn.internalClose(ctx, err)
			return
		}
	}
}

func (b *blessingsFlow) close(ctx *context.T, err error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	if err == nil {
		err = NewErrBlessingsFlowClosed(ctx, nil)
	}
	b.f.close(ctx, err)
	b.closeErr = err
	b.cond.Broadcast()
}

func dischargeList(in map[string]security.Discharge) []security.Discharge {
	out := make([]security.Discharge, 0, len(in))
	for _, d := range in {
		out = append(out, d)
	}
	return out
}
func dischargeMap(in []security.Discharge) map[string]security.Discharge {
	out := make(map[string]security.Discharge, len(in))
	for _, d := range in {
		out[d.ID()] = d
	}
	return out
}

func equalDischarges(m map[string]security.Discharge, s []security.Discharge) bool {
	if len(m) != len(s) {
		return false
	}
	for _, d := range s {
		inm, ok := m[d.ID()]
		if !ok || !d.Equivalent(inm) {
			return false
		}
	}
	return true
}
