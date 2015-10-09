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
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"
	slib "v.io/x/ref/lib/security"
)

var (
	authDialerTag   = []byte("AuthDial\x00")
	authAcceptorTag = []byte("AuthAcpt\x00")
)

func (c *Conn) dialHandshake(ctx *context.T, versions version.RPCVersionRange, auth flow.PeerAuthorizer) error {
	binding, err := c.setup(ctx, versions)
	if err != nil {
		return err
	}

	bflow := c.newFlowLocked(ctx, blessingsFlowID, 0, 0, true, true)
	bflow.releaseLocked(DefaultBytesBufferedPerFlow)
	c.blessingsFlow = newBlessingsFlow(ctx, &c.loopWG, bflow, true)

	rDischarges, err := c.readRemoteAuth(ctx, authAcceptorTag, binding)
	if err != nil {
		return err
	}
	if c.rBlessings.IsZero() {
		return NewErrAcceptorBlessingsMissing(ctx)
	}
	if !c.isProxy {
		if _, _, err := auth.AuthorizePeer(ctx, c.local, c.remote, c.rBlessings, rDischarges); err != nil {
			return verror.New(verror.ErrNotTrusted, ctx, err)
		}
	}
	signedBinding, err := v23.GetPrincipal(ctx).Sign(append(authDialerTag, binding...))
	if err != nil {
		return err
	}
	lAuth := &message.Auth{
		ChannelBinding: signedBinding,
	}
	// We only send our blessings if we are a server in addition to being a client,
	// and we are not talking through a proxy.
	// Otherwise, we only send our public key.
	if c.handler != nil && !c.isProxy {
		c.loopWG.Add(1)
		if lAuth.BlessingsKey, lAuth.DischargeKey, err = c.refreshDischarges(ctx); err != nil {
			return err
		}
	} else {
		lAuth.PublicKey = c.lBlessings.PublicKey()
	}
	return c.mp.writeMsg(ctx, lAuth)
}

func (c *Conn) acceptHandshake(ctx *context.T, versions version.RPCVersionRange) error {
	binding, err := c.setup(ctx, versions)
	if err != nil {
		return err
	}
	c.blessingsFlow = newBlessingsFlow(ctx, &c.loopWG,
		c.newFlowLocked(ctx, blessingsFlowID, 0, 0, true, true), false)
	signedBinding, err := v23.GetPrincipal(ctx).Sign(append(authAcceptorTag, binding...))
	if err != nil {
		return err
	}
	lAuth := &message.Auth{
		ChannelBinding: signedBinding,
	}
	c.loopWG.Add(1)
	if lAuth.BlessingsKey, lAuth.DischargeKey, err = c.refreshDischarges(ctx); err != nil {
		return err
	}
	if err = c.mp.writeMsg(ctx, lAuth); err != nil {
		return err
	}
	_, err = c.readRemoteAuth(ctx, authDialerTag, binding)
	return err
}

func (c *Conn) setup(ctx *context.T, versions version.RPCVersionRange) ([]byte, error) {
	pk, sk, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	lSetup := &message.Setup{
		Versions:          versions,
		PeerLocalEndpoint: c.local,
		PeerNaClPublicKey: pk,
	}
	if c.remote != nil {
		lSetup.PeerRemoteEndpoint = c.remote
	}
	ch := make(chan error)
	go func() {
		ch <- c.mp.writeMsg(ctx, lSetup)
	}()
	msg, err := c.mp.readMsg(ctx)
	if err != nil {
		<-ch
		if verror.ErrorID(err) == message.ErrWrongProtocol.ID {
			return nil, err
		}
		return nil, NewErrRecv(ctx, "unknown", err)
	}
	rSetup, valid := msg.(*message.Setup)
	if !valid {
		<-ch
		return nil, NewErrUnexpectedMsg(ctx, reflect.TypeOf(msg).String())
	}
	if err := <-ch; err != nil {
		return nil, NewErrSend(ctx, "setup", c.remote.String(), err)
	}
	if c.version, err = version.CommonVersion(ctx, lSetup.Versions, rSetup.Versions); err != nil {
		return nil, err
	}
	if c.local == nil {
		c.local = rSetup.PeerRemoteEndpoint
	}
	// We use the remote ends local endpoint as our remote endpoint when the routingID's
	// of the endpoints differ. This as an indicator to the manager that we are talking to a proxy.
	// This means that the manager will need to dial a subsequent conn on this conn
	// to the end server.
	// TODO(suharshs): Determine how to authorize the proxy.
	c.isProxy = c.remote != nil && c.remote.RoutingID() != rSetup.PeerLocalEndpoint.RoutingID()
	if c.remote == nil || c.isProxy {
		c.remote = rSetup.PeerLocalEndpoint
	}
	if rSetup.PeerNaClPublicKey == nil {
		return nil, NewErrMissingSetupOption(ctx, "peerNaClPublicKey")
	}
	binding := c.mp.setupEncryption(ctx, pk, sk, rSetup.PeerNaClPublicKey)
	// if we're encapsulated in another flow, tell that flow to stop
	// encrypting now that we've started.
	if f, ok := c.mp.rw.(*flw); ok {
		f.disableEncryption()
	}
	return binding, nil
}

func (c *Conn) readRemoteAuth(ctx *context.T, tag, binding []byte) (map[string]security.Discharge, error) {
	var rauth *message.Auth
	for {
		msg, err := c.mp.readMsg(ctx)
		if err != nil {
			return nil, NewErrRecv(ctx, c.remote.String(), err)
		}
		if rauth, _ = msg.(*message.Auth); rauth != nil {
			break
		}
		if err = c.handleMessage(ctx, msg); err != nil {
			return nil, err
		}
	}
	var rDischarges map[string]security.Discharge
	if rauth.BlessingsKey != 0 {
		var err error
		// TODO(mattr): Make sure we cancel out of this at some point.
		c.rBlessings, rDischarges, err = c.blessingsFlow.get(ctx, rauth.BlessingsKey, rauth.DischargeKey)
		if err != nil {
			return nil, err
		}
		c.rPublicKey = c.rBlessings.PublicKey()
	} else {
		c.rPublicKey = rauth.PublicKey
	}
	if c.rPublicKey == nil {
		return nil, NewErrNoPublicKey(ctx)
	}
	if !rauth.ChannelBinding.Verify(c.rPublicKey, append(tag, binding...)) {
		return nil, NewErrInvalidChannelBinding(ctx)
	}
	return rDischarges, nil
}

func (c *Conn) refreshDischarges(ctx *context.T) (bkey, dkey uint64, err error) {
	defer c.loopWG.Done()
	dis := slib.PrepareDischarges(ctx, c.lBlessings,
		security.DischargeImpetus{}, time.Minute)
	// Schedule the next update.
	dur, expires := minExpiryTime(c.lBlessings, dis)
	c.mu.Lock()
	if expires && c.status < Closing {
		c.loopWG.Add(1)
		c.dischargeTimer = time.AfterFunc(dur, func() {
			c.refreshDischarges(ctx)
		})
	}
	c.mu.Unlock()
	bkey, dkey, err = c.blessingsFlow.put(ctx, c.lBlessings, dis)
	return
}

func minExpiryTime(blessings security.Blessings, discharges map[string]security.Discharge) (time.Duration, bool) {
	var min time.Time
	cavCount := len(blessings.ThirdPartyCaveats())
	if cavCount == 0 {
		return 0, false
	}
	for _, d := range discharges {
		if exp := d.Expiry(); min.IsZero() || (!exp.IsZero() && exp.Before(min)) {
			min = exp
		}
	}
	if min.IsZero() && cavCount == len(discharges) {
		return 0, false
	}
	now := time.Now()
	d := min.Sub(now)
	if d > time.Minute && cavCount > len(discharges) {
		d = time.Minute
	}
	return d, true
}

type blessingsFlow struct {
	enc *vom.Encoder
	dec *vom.Decoder
	f   *flw

	mu      sync.Mutex
	cond    *sync.Cond
	closed  bool
	nextKey uint64
	byUID   map[uidKey]*Blessings
	byBKey  map[uint64]*Blessings
}

type uidKey struct {
	uid   string
	local bool
}

func newBlessingsFlow(ctx *context.T, loopWG *sync.WaitGroup, f *flw, dialed bool) *blessingsFlow {
	b := &blessingsFlow{
		f:       f,
		enc:     vom.NewEncoder(f),
		dec:     vom.NewDecoder(f),
		nextKey: 1,
		byUID:   make(map[uidKey]*Blessings),
		byBKey:  make(map[uint64]*Blessings),
	}
	b.cond = sync.NewCond(&b.mu)
	if !dialed {
		b.nextKey++
	}
	loopWG.Add(1)
	go b.readLoop(ctx, loopWG)
	return b
}

func (b *blessingsFlow) put(ctx *context.T, blessings security.Blessings, discharges map[string]security.Discharge) (bkey, dkey uint64, err error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	// Only return blessings that we inserted.
	buid := uidKey{string(blessings.UniqueID()), true}
	element, has := b.byUID[buid]
	if has && equalDischarges(discharges, element.Discharges) {
		return element.BKey, element.DKey, nil
	}
	defer b.cond.Broadcast()
	if has {
		element.Discharges = dischargeList(discharges)
		element.DKey = b.nextKey
		b.nextKey += 2
		return element.BKey, element.DKey, b.enc.Encode(Blessings{
			Discharges: element.Discharges,
			DKey:       element.DKey,
		})
	}
	element = &Blessings{
		Blessings:  blessings,
		Discharges: dischargeList(discharges),
		BKey:       b.nextKey,
	}
	b.nextKey += 2
	if len(discharges) > 0 {
		element.DKey = b.nextKey
		b.nextKey += 2
	}
	b.byUID[buid] = element
	b.byBKey[element.BKey] = element
	return element.BKey, element.DKey, b.enc.Encode(element)
}

func (b *blessingsFlow) get(ctx *context.T, bkey, dkey uint64) (security.Blessings, map[string]security.Discharge, error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	for {
		element, has := b.byBKey[bkey]
		if has && element.DKey == dkey {
			return element.Blessings, dischargeMap(element.Discharges), nil
		}
		// We check b.closed after we check the map to allow gets to succeed even after
		// the blessingsFlow is closed.
		if b.closed {
			break
		}
		b.cond.Wait()
	}
	return security.Blessings{}, nil, NewErrBlessingsFlowClosed(ctx)
}

func (b *blessingsFlow) getLatestDischarges(ctx *context.T, blessings security.Blessings, local bool) (map[string]security.Discharge, error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	buid := uidKey{string(blessings.UniqueID()), local}
	for !b.closed {
		element, has := b.byUID[buid]
		if has {
			return dischargeMap(element.Discharges), nil
		}
		b.cond.Wait()
	}
	return nil, NewErrBlessingsFlowClosed(ctx)
}

func (b *blessingsFlow) readLoop(ctx *context.T, loopWG *sync.WaitGroup) {
	defer loopWG.Done()
	for {
		var received Blessings
		err := b.dec.Decode(&received)
		b.mu.Lock()
		if err != nil {
			if err != io.EOF {
				// TODO(mattr): In practice this is very spammy,
				// figure out how to log it more effectively.
				ctx.VI(3).Infof("Blessings flow closed: %v", err)
			}
			b.closed = true
			b.mu.Unlock()
			return
		}
		// When accepting, make sure the blessings received are bound to the conn's
		// remote public key.
		// Someone is trying to be use our blessings.
		if received.BKey%2 == b.nextKey%2 {
			b.mu.Unlock()
			b.f.conn.mu.Lock()
			b.f.conn.internalCloseLocked(ctx, NewErrBlessingsNotBound(ctx))
			b.f.conn.mu.Unlock()
			return
		}
		if b.f.conn.rPublicKey != nil && received.BKey != 0 {
			if !reflect.DeepEqual(received.Blessings.PublicKey(), b.f.conn.rPublicKey) {
				b.mu.Unlock()
				b.f.conn.mu.Lock()
				b.f.conn.internalCloseLocked(ctx, NewErrBlessingsNotBound(ctx))
				b.f.conn.mu.Unlock()
				return
			}
		}
		b.byUID[uidKey{string(received.Blessings.UniqueID()), false}] = &received
		b.byBKey[received.BKey] = &received
		b.cond.Broadcast()
		b.mu.Unlock()
	}
}

func (b *blessingsFlow) close(ctx *context.T, err error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	b.f.close(ctx, err)
	b.closed = true
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
		if !d.Equivalent(m[d.ID()]) {
			return false
		}
	}
	return true
}
