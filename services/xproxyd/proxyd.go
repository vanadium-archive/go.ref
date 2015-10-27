// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd

import (
	"io"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/naming"
)

const reconnectDelay = 50 * time.Millisecond

type proxy struct {
	m              flow.Manager
	mu             sync.Mutex
	proxyEndpoints map[string][]naming.Endpoint // keyed by proxy address
}

func New(ctx *context.T) (*proxy, *context.T, error) {
	mgr, err := v23.NewFlowManager(ctx)
	if err != nil {
		return nil, nil, err
	}
	p := &proxy{
		m:              mgr,
		proxyEndpoints: make(map[string][]naming.Endpoint),
	}
	for _, addr := range v23.GetListenSpec(ctx).Addrs {
		if addr.Protocol == "v23" {
			ep, err := v23.NewEndpoint(addr.Address)
			if err != nil {
				return nil, nil, err
			}
			go p.connectToProxy(ctx, addr.Address, ep)
		} else if err := p.m.Listen(ctx, addr.Protocol, addr.Address); err != nil {
			return nil, nil, err
		}
	}
	go p.listenLoop(ctx)
	return p, ctx, nil
}

func (p *proxy) ListeningEndpoints() []naming.Endpoint {
	// TODO(suharshs): Return changed channel here as well.
	eps, _ := p.m.ListeningEndpoints()
	return eps
}

func (p *proxy) MultipleProxyEndpoints() []naming.Endpoint {
	var eps []naming.Endpoint
	p.mu.Lock()
	for _, v := range p.proxyEndpoints {
		eps = append(eps, v...)
	}
	p.mu.Unlock()
	return eps
}

func (p *proxy) listenLoop(ctx *context.T) {
	for {
		f, err := p.m.Accept(ctx)
		if err != nil {
			ctx.Infof("p.m.Accept failed: %v", err)
			break
		}
		msg, err := readMessage(ctx, f)
		if err != nil {
			ctx.Errorf("reading message failed: %v", err)
		}
		switch m := msg.(type) {
		case *message.Setup:
			err = p.startRouting(ctx, f, m)
		case *message.MultiProxyRequest:
			err = p.replyToProxy(ctx, f)
		case *message.ProxyServerRequest:
			err = p.replyToServer(ctx, f)
		default:
			continue
		}
		if err != nil {
			ctx.Errorf("failed to handle incoming connection: %v", err)
		}
	}
}

func (p *proxy) startRouting(ctx *context.T, f flow.Flow, m *message.Setup) error {
	fout, err := p.dialNextHop(ctx, f, m)
	if err != nil {
		f.Close()
		return err
	}
	go p.forwardLoop(ctx, f, fout)
	go p.forwardLoop(ctx, fout, f)
	return nil
}

func (p *proxy) forwardLoop(ctx *context.T, fin, fout flow.Flow) {
	_, err := io.Copy(fin, fout)
	fin.Close()
	fout.Close()
	ctx.Errorf("f.Read failed: %v", err)
}

func (p *proxy) dialNextHop(ctx *context.T, f flow.Flow, m *message.Setup) (flow.Flow, error) {
	var (
		rid naming.RoutingID
		ep  naming.Endpoint
		err error
	)
	if ep, err = setBidiProtocol(m.PeerRemoteEndpoint); err != nil {
		return nil, err
	}
	if routes := ep.Routes(); len(routes) > 0 {
		if err := rid.FromString(routes[0]); err != nil {
			return nil, err
		}
		// Make an endpoint with the correct routingID to dial out. All other fields
		// do not matter.
		// TODO(suharshs): Make sure that the routingID from the route belongs to a
		// connection that is stored in the manager's cache. (i.e. a Server has connected
		// with the routingID before)
		if ep, err = setEndpointRoutingID(ep, rid); err != nil {
			return nil, err
		}
		// Remove the read route from the setup message endpoint.
		if m.PeerRemoteEndpoint, err = setEndpointRoutes(m.PeerRemoteEndpoint, routes[1:]); err != nil {
			return nil, err
		}
	}
	fout, err := p.m.Dial(ctx, ep, proxyAuthorizer{})
	if err != nil {
		return nil, err
	}
	// Write the setup message back onto the flow for the next hop to read.
	return fout, writeMessage(ctx, m, fout)
}

func (p *proxy) replyToServer(ctx *context.T, f flow.Flow) error {
	rid := f.RemoteEndpoint().RoutingID()
	eps, err := p.returnEndpoints(ctx, rid, "")
	if err != nil {
		return err
	}
	return writeMessage(ctx, &message.ProxyResponse{Endpoints: eps}, f)
}

func (p *proxy) replyToProxy(ctx *context.T, f flow.Flow) error {
	// Add the routing id of the incoming proxy to the routes. The routing id of the
	// returned endpoint doesn't matter because it will eventually be replaced
	// by a server's rid by some later proxy.
	// TODO(suharshs): Use a local route instead of this global routingID.
	rid := f.RemoteEndpoint().RoutingID()
	eps, err := p.returnEndpoints(ctx, naming.NullRoutingID, rid.String())
	if err != nil {
		return err
	}
	return writeMessage(ctx, &message.ProxyResponse{Endpoints: eps}, f)
}

func (p *proxy) returnEndpoints(ctx *context.T, rid naming.RoutingID, route string) ([]naming.Endpoint, error) {
	p.mu.Lock()
	eps, _ := p.m.ListeningEndpoints()
	for _, peps := range p.proxyEndpoints {
		eps = append(eps, peps...)
	}
	p.mu.Unlock()
	if len(eps) == 0 {
		return nil, NewErrNotListening(ctx)
	}
	for idx, ep := range eps {
		var err error
		if rid != naming.NullRoutingID {
			ep, err = setEndpointRoutingID(ep, rid)
			if err != nil {
				return nil, err
			}
		}
		if len(route) > 0 {
			var cp []string
			cp = append(cp, ep.Routes()...)
			cp = append(cp, route)
			ep, err = setEndpointRoutes(ep, cp)
			if err != nil {
				return nil, err
			}
		}
		eps[idx] = ep
	}
	return eps, nil
}

func (p *proxy) connectToProxy(ctx *context.T, address string, ep naming.Endpoint) {
	for delay := reconnectDelay; ; delay *= 2 {
		time.Sleep(delay - reconnectDelay)
		select {
		case <-ctx.Done():
			return
		default:
		}
		f, err := p.m.Dial(ctx, ep, proxyAuthorizer{})
		if err != nil {
			ctx.Error(err)
			continue
		}
		// Send a byte telling the acceptor that we are a proxy.
		if err := writeMessage(ctx, &message.MultiProxyRequest{}, f); err != nil {
			ctx.Error(err)
			continue
		}
		eps, err := readProxyResponse(ctx, f)
		if err != nil {
			ctx.Error(err)
			continue
		}
		p.mu.Lock()
		p.proxyEndpoints[address] = eps
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-f.Closed():
			p.mu.Lock()
			delete(p.proxyEndpoints, address)
			p.mu.Unlock()
			delay = reconnectDelay
		}
	}
}
