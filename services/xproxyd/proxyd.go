// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd

import (
	"fmt"
	"io"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	"v.io/v23/naming"
)

// TODO(suharshs): Make sure that we don't leak any goroutines.

type proxy struct {
	m              flow.Manager
	mu             sync.Mutex
	proxyEndpoints []naming.Endpoint
}

func New(ctx *context.T) (*proxy, *context.T, error) {
	ctx, mgr, err := v23.ExperimentalWithNewFlowManager(ctx)
	if err != nil {
		return nil, nil, err
	}
	p := &proxy{
		m: mgr,
	}
	for _, addr := range v23.GetListenSpec(ctx).Addrs {
		if addr.Protocol == "v23" {
			ep, err := v23.NewEndpoint(addr.Address)
			if err != nil {
				return nil, nil, err
			}
			f, err := p.m.Dial(ctx, ep, proxyBlessingsForPeer{}.run)
			if err != nil {
				return nil, nil, err
			}
			// Send a byte telling the acceptor that we are a proxy.
			if err := writeMessage(ctx, &message.MultiProxyRequest{}, f); err != nil {
				return nil, nil, err
			}
			msg, err := readMessage(ctx, f)
			if err != nil {
				return nil, nil, err
			}
			m, ok := msg.(*message.ProxyResponse)
			if !ok {
				return nil, nil, NewErrUnexpectedMessage(ctx, fmt.Sprintf("%t", m))
			}
			p.mu.Lock()
			p.proxyEndpoints = append(p.proxyEndpoints, m.Endpoints...)
			p.mu.Unlock()
		} else if err := p.m.Listen(ctx, addr.Protocol, addr.Address); err != nil {
			return nil, nil, err
		}
	}
	go p.listenLoop(ctx)
	return p, ctx, nil
}

func (p *proxy) ListeningEndpoints() []naming.Endpoint {
	return p.m.ListeningEndpoints()
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
			ctx.Errorf("reading type byte failed: %v", err)
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
		return err
	}
	go p.forwardLoop(ctx, f, fout)
	go p.forwardLoop(ctx, fout, f)
	return nil
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

func (p *proxy) dialNextHop(ctx *context.T, f flow.Flow, m *message.Setup) (flow.Flow, error) {
	var (
		rid naming.RoutingID
		ep  naming.Endpoint
		err error
	)
	if routes := m.PeerRemoteEndpoint.Routes(); len(routes) > 0 {
		if err := rid.FromString(routes[0]); err != nil {
			return nil, err
		}
		// Make an endpoint with the correct routingID to dial out. All other fields
		// do not matter.
		// TODO(suharshs): Make sure that the routingID from the route belongs to a
		// connection that is stored in the manager's cache. (i.e. a Server has connected
		// with the routingID before)
		if ep, err = setEndpointRoutingID(m.PeerRemoteEndpoint, rid); err != nil {
			return nil, err
		}
		// Remove the read route from the setup message endpoint.
		if m.PeerRemoteEndpoint, err = setEndpointRoutes(m.PeerRemoteEndpoint, routes[1:]); err != nil {
			return nil, err
		}
	} else {
		ep = m.PeerRemoteEndpoint
	}
	fout, err := p.m.Dial(ctx, ep, proxyBlessingsForPeer{}.run)
	if err != nil {
		return nil, err
	}
	// Write the setup message back onto the flow for the next hop to read.
	return fout, writeMessage(ctx, m, fout)
}

func (p *proxy) replyToServer(ctx *context.T, f flow.Flow) error {
	rid := f.Conn().RemoteEndpoint().RoutingID()
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
	rid := f.Conn().RemoteEndpoint().RoutingID()
	eps, err := p.returnEndpoints(ctx, naming.NullRoutingID, rid.String())
	if err != nil {
		return err
	}
	return writeMessage(ctx, &message.ProxyResponse{Endpoints: eps}, f)
}

func (p *proxy) returnEndpoints(ctx *context.T, rid naming.RoutingID, route string) ([]naming.Endpoint, error) {
	p.mu.Lock()
	eps := append(p.m.ListeningEndpoints(), p.proxyEndpoints...)
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
