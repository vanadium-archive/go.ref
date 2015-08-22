// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd

import (
	"io"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/vom"
)

// TODO(suharshs): Make sure that we don't leak any goroutines.

type proxy struct {
	m flow.Manager
}

func New(ctx *context.T) (*proxy, error) {
	p := &proxy{
		m: v23.ExperimentalGetFlowManager(ctx),
	}
	for _, addr := range v23.GetListenSpec(ctx).Addrs {
		ctx.Infof("proxy listening on %v", addr)
		if err := p.m.Listen(ctx, addr.Protocol, addr.Address); err != nil {
			return nil, err
		}
	}
	go p.listenLoop(ctx)
	return p, nil
}

func (p *proxy) listenLoop(ctx *context.T) {
	for {
		f, err := p.m.Accept(ctx)
		if err != nil {
			ctx.Infof("p.m.Accept failed: %v", err)
			break
		}
		if p.shouldRoute(f) {
			err = p.startRouting(ctx, f)
		} else {
			err = p.replyToServer(ctx, f)
		}
		if err != nil {
			ctx.Errorf("failed to handle incoming connection: %v", err)
		}
	}
}

func (p *proxy) startRouting(ctx *context.T, f flow.Flow) error {
	rid, err := p.readRouteInfo(ctx, f)
	if err != nil {
		return err
	}
	// TODO(suharshs): Find a better way to do this.
	ep, err := v23.NewEndpoint("@6@@@@" + rid.String() + "@@@@")
	if err != nil {
		return err
	}
	fout, err := p.m.Dial(ctx, ep, proxyBlessingsForPeer{}.run)
	if err != nil {
		return err
	}
	go p.forwardLoop(ctx, f, fout)
	go p.forwardLoop(ctx, fout, f)
	return nil
}

type proxyBlessingsForPeer struct{}

// TODO(suharshs): Figure out what blessings to present here. And present discharges.
func (proxyBlessingsForPeer) run(ctx *context.T, lep, rep naming.Endpoint, rb security.Blessings,
	rd map[string]security.Discharge) (security.Blessings, map[string]security.Discharge, error) {
	return v23.GetPrincipal(ctx).BlessingStore().Default(), nil, nil
}

func (p *proxy) replyToServer(ctx *context.T, f flow.Flow) error {
	eps := p.ListeningEndpoints()
	if len(eps) == 0 {
		return NewErrNotListening(ctx)
	}
	// TODO(suharshs): handle listening on multiple endpoints.
	ep := eps[0]
	network, address := ep.Addr().Network(), ep.Addr().String()
	// TODO(suharshs): deal with routes and such here, if we are replying to a proxy.
	rid := f.Conn().RemoteEndpoint().RoutingID()
	epString := naming.FormatEndpoint(network, address, rid)
	if err := vom.NewEncoder(f).Encode(epString); err != nil {
		return err
	}
	return nil
}

func (p *proxy) ListeningEndpoints() []naming.Endpoint {
	return p.m.ListeningEndpoints()
}

func (p *proxy) forwardLoop(ctx *context.T, fin, fout flow.Flow) {
	for {
		_, err := io.Copy(fin, fout)
		if err == io.EOF {
			return
		} else if err != nil {
			ctx.Errorf("f.Read failed: %v", err)
			return
		}
	}
}

func (p *proxy) readRouteInfo(ctx *context.T, f flow.Flow) (naming.RoutingID, error) {
	// TODO(suharshs): Read route information here when implementing multi proxy.
	var (
		rid string
		ret naming.RoutingID
	)
	if err := vom.NewDecoder(f).Decode(&rid); err != nil {
		return ret, err
	}
	err := ret.FromString(rid)
	return ret, err
}

func (p *proxy) shouldRoute(f flow.Flow) bool {
	rid := f.Conn().LocalEndpoint().RoutingID()
	return rid != p.m.RoutingID() && rid != naming.NullRoutingID
}
