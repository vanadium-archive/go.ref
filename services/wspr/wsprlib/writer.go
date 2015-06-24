// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wsprlib

import (
	"fmt"
	"path/filepath"
	"runtime"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/ref/services/wspr/internal/app"
	"v.io/x/ref/services/wspr/internal/lib"

	"github.com/gorilla/websocket"
)

// Wraps a response to the proxy client and adds a message type.
type response struct {
	Type    lib.ResponseType
	Message interface{}
}

// Implements clientWriter interface for sending messages over websockets.
type websocketWriter struct {
	p   *pipe
	id  int32
	ctx *context.T
}

func (w *websocketWriter) Send(messageType lib.ResponseType, data interface{}) error {
	msg, err := app.ConstructOutgoingMessage(w.id, messageType, data)
	if err != nil {
		return err
	}

	w.p.writeQueue <- wsMessage{messageType: websocket.TextMessage, buf: []byte(msg)}

	return nil
}

func (w *websocketWriter) Error(err error) {
	verr := verror.Convert(verror.ErrUnknown, nil, err)

	// Also log the error but write internal errors at a more severe log level
	logLevel := 2
	logErr := fmt.Sprintf("%v", verr)

	// Prefix the message with the code locations associated with verr,
	// except the last, which is the Convert() above.  This does nothing if
	// err was not a verror error.
	verrStack := verror.Stack(verr)
	for i := 0; i < len(verrStack)-1; i++ {
		pc := verrStack[i]
		fnc := runtime.FuncForPC(pc)
		file, line := fnc.FileLine(pc)
		logErr = fmt.Sprintf("%s:%d: %s", file, line, logErr)
	}

	// We want to look at the stack three frames up to find where the error actually
	// occurred.  (caller -> websocketErrorResponse/sendError -> generateErrorMessage).
	if _, file, line, ok := runtime.Caller(3); ok {
		logErr = fmt.Sprintf("%s:%d: %s", filepath.Base(file), line, logErr)
	}
	if verror.ErrorID(verr) == verror.ErrInternal.ID {
		logLevel = 2
	}
	w.ctx.VI(logLevel).Info(logErr)

	w.Send(lib.ResponseError, verr)
}
