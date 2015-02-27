package browspr

import (
	"encoding/json"
	"fmt"

	"v.io/core/veyron/services/wsprd/app"
	"v.io/core/veyron/services/wsprd/lib"
	"v.io/v23/vlog"
)

// pipe controls the flow of messages for a specific instance (corresponding to a specific tab).
type pipe struct {
	browspr    *Browspr
	controller *app.Controller
	origin     string
	instanceId int32
}

func newPipe(b *Browspr, instanceId int32, origin string) *pipe {
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
		vlog.Errorf("No principal associated with origin %v, creating a new principal with self-signed blessing from browspr: %v", origin, err)

		dummyAccount, err := b.principalManager.DummyAccount()
		if err != nil {
			vlog.Errorf("principalManager.DummyAccount() failed: %v", err)
			return nil
		}

		if err := b.accountManager.AssociateAccount(origin, dummyAccount, nil); err != nil {
			vlog.Errorf("accountManager.AssociateAccount(%v, %v, %v) failed: %v", origin, dummyAccount, nil, err)
			return nil
		}
		p, err = b.accountManager.LookupPrincipal(origin)
		if err != nil {
			return nil
		}
	}

	pipe.controller, err = app.NewController(b.ctx, pipe.createWriter, b.listenSpec, b.namespaceRoots, p)
	if err != nil {
		vlog.Errorf("Could not create controller: %v", err)
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

func (p *pipe) cleanup() {
	vlog.VI(0).Info("Cleaning up pipe")
	p.controller.Cleanup()
}

func (p *pipe) handleMessage(jsonMsg string) error {
	var msg app.Message
	if err := json.Unmarshal([]byte(jsonMsg), &msg); err != nil {
		fullErr := fmt.Errorf("Can't unmarshall message: %s error: %v", jsonMsg, err)
		// Send the failed to unmarshal error to the client.
		errWriter := &postMessageWriter{p: p}
		errWriter.Error(fullErr)
		return fullErr
	}

	writer := p.createWriter(msg.Id)
	p.controller.HandleIncomingMessage(msg, writer)
	return nil
}
