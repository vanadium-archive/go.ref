// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package browspr

import (
	"v.io/x/ref/services/wspr/internal/app"
	"v.io/x/ref/services/wspr/internal/lib"
)

// postMessageWriter is a lib.ClientWriter that handles sending messages over postMessage to the extension.
type postMessageWriter struct {
	messageId int32
	p         *pipe
}

func (w *postMessageWriter) Send(messageType lib.ResponseType, data interface{}) error {
	outMsg, err := app.ConstructOutgoingMessage(w.messageId, messageType, data)
	if err != nil {
		return err
	}
	w.p.browspr.postMessage(w.p.instanceId, "browsprMsg", outMsg)
	return nil
}

func (w *postMessageWriter) Error(err error) {
	w.Send(lib.ResponseError, app.FormatAsVerror(w.p.browspr.ctx, err))
}
