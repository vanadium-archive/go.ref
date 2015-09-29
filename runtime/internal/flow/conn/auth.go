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

func (c *Conn) dialHandshake(ctx *context.T, versions version.RPCVersionRange) error {
	binding, err := c.setup(ctx, versions)
	if err != nil {
		return err
	}
	bflow := c.newFlowLocked(ctx, blessingsFlowID, 0, 0, true, true)
	bflow.worker.Release(ctx, defaultBufferSize)
	c.blessingsFlow = newBlessingsFlow(ctx, &c.loopWG, bflow, true)

	if err = c.readRemoteAuth(ctx, authAcceptorTag, binding); err != nil {
		return err
	}
	if c.rBlessings.IsZero() {
		return NewErrAcceptorBlessingsMissing(ctx)
	}
	signedBinding, err := v23.GetPrincipal(ctx).Sign(append(authDialerTag, binding...))
	if err != nil {
		return err
	}
	lAuth := &message.Auth{
		ChannelBinding: signedBinding,
	}
	// We only send our blessings if we are a server in addition to being a client.
	// If we are a pure client, we only send our public key.
	if c.handler != nil {
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
	if lAuth.BlessingsKey, lAuth.DischargeKey, err = c.refreshDischarges(ctx); err != nil {
		return err
	}
	if err = c.mp.writeMsg(ctx, lAuth); err != nil {
		return err
	}
	return c.readRemoteAuth(ctx, authDialerTag, binding)
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
	// TODO(mattr): Decide which endpoints to actually keep, the ones we know locally
	// or what the remote side thinks.
	if rSetup.PeerRemoteEndpoint != nil {
		c.local = rSetup.PeerRemoteEndpoint
	}
	if rSetup.PeerLocalEndpoint != nil {
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

func (c *Conn) readRemoteAuth(ctx *context.T, tag []byte, binding []byte) error {
	var rauth *message.Auth
	for {
		msg, err := c.mp.readMsg(ctx)
		if err != nil {
			return NewErrRecv(ctx, c.remote.String(), err)
		}
		if rauth, _ = msg.(*message.Auth); rauth != nil {
			break
		}
		if err = c.handleMessage(ctx, msg); err != nil {
			return err
		}
	}
	if rauth.BlessingsKey != 0 {
		var err error
		// TODO(mattr): Make sure we cancel out of this at some point.
		c.rBlessings, _, err = c.blessingsFlow.get(ctx, rauth.BlessingsKey, rauth.DischargeKey)
		if err != nil {
			return err
		}
		c.rPublicKey = c.rBlessings.PublicKey()
	} else {
		c.rPublicKey = rauth.PublicKey
	}
	if c.rPublicKey == nil {
		return NewErrNoPublicKey(ctx)
	}
	if !rauth.ChannelBinding.Verify(c.rPublicKey, append(tag, binding...)) {
		return NewErrInvalidChannelBinding(ctx)
	}
	return nil
}

func (c *Conn) refreshDischarges(ctx *context.T) (bkey, dkey uint64, err error) {
	dis := slib.PrepareDischarges(ctx, c.lBlessings,
		security.DischargeImpetus{}, time.Minute)
	// Schedule the next update.
	var timer *time.Timer
	if dur, expires := minExpiryTime(c.lBlessings, dis); expires {
		timer = time.AfterFunc(dur, func() {
			c.refreshDischarges(ctx)
		})
	}
	bkey, dkey, err = c.blessingsFlow.put(ctx, c.lBlessings, dis)
	c.mu.Lock()
	c.dischargeTimer = timer
	c.mu.Unlock()
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

	mu      sync.Mutex
	cond    *sync.Cond
	closed  bool
	nextKey uint64
	byUID   map[string]*Blessings
	byBKey  map[uint64]*Blessings
}

func newBlessingsFlow(ctx *context.T, loopWG *sync.WaitGroup, f flow.Flow, dialed bool) *blessingsFlow {
	b := &blessingsFlow{
		enc:     vom.NewEncoder(f),
		dec:     vom.NewDecoder(f),
		nextKey: 1,
		byUID:   make(map[string]*Blessings),
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
	buid := string(blessings.UniqueID())
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
	for !b.closed {
		element, has := b.byBKey[bkey]
		if has && element.DKey == dkey {
			return element.Blessings, dischargeMap(element.Discharges), nil
		}
		b.cond.Wait()
	}
	return security.Blessings{}, nil, NewErrBlessingsFlowClosed(ctx)
}

func (b *blessingsFlow) getLatestDischarges(ctx *context.T, blessings security.Blessings) (map[string]security.Discharge, error) {
	defer b.mu.Unlock()
	b.mu.Lock()
	buid := string(blessings.UniqueID())
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
		b.byUID[string(received.Blessings.UniqueID())] = &received
		b.byBKey[received.BKey] = &received
		b.cond.Broadcast()
		b.mu.Unlock()
	}
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
