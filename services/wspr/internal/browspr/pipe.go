// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package browspr

import (
	"v.io/v23/context"
	"v.io/x/ref/services/wspr/internal/app"
	"v.io/x/ref/services/wspr/internal/lib"
)

// pipe controls the flow of messages for a specific instance (corresponding to a specific tab).
type pipe struct {
	browspr    *Browspr
	controller *app.Controller
	origin     string
	instanceId int32
}

func newPipe(b *Browspr, instanceId int32, origin string, namespaceRoots []string, proxy string) *pipe {
	pipe := &pipe{
		browspr:    b,
		origin:     origin,
		instanceId: instanceId,
	}

	// TODO(bprosnitz) LookupPrincipal() maybe should not take a string in the future.
	p, err := b.accountManager.LookupPrincipal(origin)
	if err != nil {
		// TODO(nlacasse, bjornick): This code should go away once we
		// start requiring authentication.  At that point, we should
		// just return an error to the client.
		b.ctx.Errorf("No principal associated with origin %v, creating a new principal with self-signed blessing from browspr: %v", origin, err)

		dummyAccount, err := b.principalManager.DummyAccount()
		if err != nil {
			b.ctx.Errorf("principalManager.DummyAccount() failed: %v", err)
			return nil
		}

		if err := b.accountManager.AssociateAccount(b.ctx, origin, dummyAccount, nil); err != nil {
			b.ctx.Errorf("accountManager.AssociateAccount(%v, %v, %v) failed: %v", origin, dummyAccount, nil, err)
			return nil
		}
		p, err = b.accountManager.LookupPrincipal(origin)
		if err != nil {
			return nil
		}
	}

	// Shallow copy browspr's default listenSpec.  If we have passed in a
	// proxy, set listenSpec.proxy.
	listenSpec := *b.listenSpec
	if proxy != "" {
		listenSpec.Proxy = proxy
	}

	// If we have been passed in namespace roots, pass them to NewController, otherwise pass browspr's default.
	if namespaceRoots == nil {
		namespaceRoots = b.namespaceRoots
	}

	pipe.controller, err = app.NewController(b.ctx, pipe.createWriter, &listenSpec, namespaceRoots, p)
	if err != nil {
		b.ctx.Errorf("Could not create controller: %v", err)
		return nil
	}

	return pipe
}

func (p *pipe) createWriter(messageId int32) lib.ClientWriter {
	return &postMessageWriter{
		messageId: messageId,
		p:         p,
	}
}

func (p *pipe) cleanup(ctx *context.T) {
	ctx.VI(0).Info("Cleaning up pipe")
	p.controller.Cleanup(ctx)
}

func (p *pipe) handleMessage(msg app.Message) error {
	writer := p.createWriter(msg.Id)
	p.controller.HandleIncomingMessage(msg, writer)
	return nil
}
