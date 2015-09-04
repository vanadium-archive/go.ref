// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xproxyd

import (
	"io"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/naming"
	"v.io/v23/vom"
)

// TODO(suharshs): Make sure that we don't leak any goroutines.

const proxyByte = byte('p')
const serverByte = byte('s')
const clientByte = byte('c')

type proxy struct {
	m              flow.Manager
	mu             sync.Mutex
	proxyEndpoints []naming.Endpoint
}

func New(ctx *context.T) (*proxy, error) {
	p := &proxy{
		m: v23.ExperimentalGetFlowManager(ctx),
	}
	for _, addr := range v23.GetListenSpec(ctx).Addrs {
		if addr.Protocol == "v23" {
			ep, err := v23.NewEndpoint(addr.Address)
			if err != nil {
				return nil, err
			}
			f, err := p.m.Dial(ctx, ep, proxyBlessingsForPeer{}.run)
			if err != nil {
				return nil, err
			}
			// Send a byte telling the acceptor that we are a proxy.
			if _, err := f.Write([]byte{proxyByte}); err != nil {
				return nil, err
			}
			var lep string
			if err := vom.NewDecoder(f).Decode(&lep); err != nil {
				return nil, err
			}
			proxyEndpoint, err := v23.NewEndpoint(lep)
			if err != nil {
				return nil, err
			}
			p.mu.Lock()
			p.proxyEndpoints = append(p.proxyEndpoints, proxyEndpoint)
			p.mu.Unlock()
		} else if err := p.m.Listen(ctx, addr.Protocol, addr.Address); err != nil {
			return nil, err
		}
	}
	go p.listenLoop(ctx)
	return p, nil
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
		msg := make([]byte, 1)
		if _, err := f.Read(msg); err != nil {
			ctx.Errorf("reading type byte failed: %v", err)
		}
		switch msg[0] {
		case clientByte:
			err = p.startRouting(ctx, f)
		case proxyByte:
			err = p.replyToProxy(ctx, f)
		case serverByte:
			err = p.replyToServer(ctx, f)
		default:
			continue
		}
		if err != nil {
			ctx.Errorf("failed to handle incoming connection: %v", err)
		}
	}
}

func (p *proxy) startRouting(ctx *context.T, f flow.Flow) error {
	fout, err := p.dialNextHop(ctx, f)
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

func (p *proxy) dialNextHop(ctx *context.T, f flow.Flow) (flow.Flow, error) {
	m, err := readSetupMessage(ctx, f)
	if err != nil {
		return nil, err
	}
	var rid naming.RoutingID
	var ep naming.Endpoint
	var shouldWriteClientByte bool
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
		shouldWriteClientByte = true
	} else {
		ep = m.PeerRemoteEndpoint
	}
	fout, err := p.m.Dial(ctx, ep, proxyBlessingsForPeer{}.run)
	if err != nil {
		return nil, err
	}
	if shouldWriteClientByte {
		// We only write the clientByte on flows made to proxys. If we are creating
		// the last hop flow to the end server, we don't need to send the byte.
		if _, err := fout.Write([]byte{clientByte}); err != nil {
			return nil, err
		}
	}

	// Write the setup message back onto the flow for the next hop to read.
	return fout, writeSetupMessage(ctx, m, fout)
}

func (p *proxy) replyToServer(ctx *context.T, f flow.Flow) error {
	rid := f.Conn().RemoteEndpoint().RoutingID()
	epString, err := p.returnEndpoint(ctx, rid, "")
	if err != nil {
		return err
	}
	// TODO(suharshs): Make a low-level message for this information instead of
	// VOM-Encoding the endpoint string.
	return vom.NewEncoder(f).Encode(epString)
}

func (p *proxy) replyToProxy(ctx *context.T, f flow.Flow) error {
	// Add the routing id of the incoming proxy to the routes. The routing id of the
	// returned endpoint doesn't matter because it will eventually be replaced
	// by a server's rid by some later proxy.
	// TODO(suharshs): Use a local route instead of this global routingID.
	rid := f.Conn().RemoteEndpoint().RoutingID()
	epString, err := p.returnEndpoint(ctx, naming.NullRoutingID, rid.String())
	if err != nil {
		return err
	}
	return vom.NewEncoder(f).Encode(epString)
}

func (p *proxy) returnEndpoint(ctx *context.T, rid naming.RoutingID, route string) (string, error) {
	p.mu.Lock()
	eps := append(p.m.ListeningEndpoints(), p.proxyEndpoints...)
	p.mu.Unlock()
	if len(eps) == 0 {
		return "", NewErrNotListening(ctx)
	}
	// TODO(suharshs): handle listening on multiple endpoints.
	ep := eps[len(eps)-1]
	var err error
	if rid != naming.NullRoutingID {
		ep, err = setEndpointRoutingID(ep, rid)
		if err != nil {
			return "", err
		}
	}
	if len(route) > 0 {
		var cp []string
		cp = append(cp, ep.Routes()...)
		cp = append(cp, route)
		ep, err = setEndpointRoutes(ep, cp)
		if err != nil {
			return "", err
		}
	}
	return ep.String(), nil
}
