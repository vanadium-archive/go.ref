package browspr

import (
	"encoding/json"
	"fmt"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/wspr/veyron/services/wsprd/app"
	"veyron.io/wspr/veyron/services/wsprd/lib"
)

// pipe controls the flow of messages for a specific instance (corresponding to a specific tab).
type pipe struct {
	browspr    *Browspr
	controller *app.Controller
	instanceId int32
}

func newPipe(b *Browspr, instanceId int32) *pipe {
	pipe := &pipe{
		browspr:    b,
		instanceId: instanceId,
	}

	// TODO(bprosnitz) Principal() maybe should not take a string in the future.
	p, err := b.accountManager.LookupPrincipal(fmt.Sprintf("%d", instanceId))
	if err != nil {
		p = b.rt.Principal()
		b.rt.Logger().Errorf("No principal associated with instanceId %d: %v", instanceId, err)
		// TODO(bjornick): Send an error to the client when all of the principal stuff is set up.
	}

	var profile veyron2.Profile
	if b.profileFactory != nil {
		profile = b.profileFactory()
	}
	pipe.controller, err = app.NewController(pipe.createWriter, profile, &b.listenSpec, b.namespaceRoots, options.RuntimePrincipal{p})
	if err != nil {
		b.rt.Logger().Errorf("Could not create controller: %v", err)
		return nil
	}

	return pipe
}

func (p *pipe) createWriter(messageId int64) lib.ClientWriter {
	return &postMessageWriter{
		messageId: messageId,
		p:         p,
	}
}

func (p *pipe) cleanup() {
	p.browspr.logger.VI(0).Info("Cleaning up websocket")
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
	context := p.browspr.rt.NewContext()
	p.controller.HandleIncomingMessage(context, msg, writer)
	return nil
}
