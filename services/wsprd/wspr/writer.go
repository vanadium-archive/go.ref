package wspr

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"

	"veyron.io/veyron/veyron/services/wsprd/lib"

	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"

	"github.com/gorilla/websocket"
)

// Wraps a response to the proxy client and adds a message type.
type response struct {
	Type    lib.ResponseType
	Message interface{}
}

// Implements clientWriter interface for sending messages over websockets.
type websocketWriter struct {
	p      *pipe
	logger vlog.Logger
	id     int64
}

func (w *websocketWriter) Send(messageType lib.ResponseType, data interface{}) error {
	var buf bytes.Buffer
	if err := vom.ObjToJSON(&buf, vom.ValueOf(response{Type: messageType, Message: data})); err != nil {
		w.logger.Error("Failed to marshal with", err)
		return err
	}

	var buf2 bytes.Buffer

	if err := vom.ObjToJSON(&buf2, vom.ValueOf(websocketMessage{Id: w.id, Data: buf.String()})); err != nil {
		w.logger.Error("Failed to write the message", err)
		return err
	}

	w.p.writeQueue <- wsMessage{messageType: websocket.TextMessage, buf: buf2.Bytes()}

	return nil
}

func (w *websocketWriter) Error(err error) {
	verr := verror.ToStandard(err)

	// Also log the error but write internal errors at a more severe log level
	var logLevel vlog.Level = 2
	logErr := fmt.Sprintf("%v", verr)
	// We want to look at the stack three frames up to find where the error actually
	// occurred.  (caller -> websocketErrorResponse/sendError -> generateErrorMessage).
	if _, file, line, ok := runtime.Caller(3); ok {
		logErr = fmt.Sprintf("%s:%d: %s", filepath.Base(file), line, logErr)
	}
	if verror.Is(verr, verror.Internal) {
		logLevel = 2
	}
	w.logger.VI(logLevel).Info(logErr)

	var errMsg = verror.Standard{
		ID:  verr.ErrorID(),
		Msg: verr.Error(),
	}

	w.Send(lib.ResponseError, errMsg)
}
