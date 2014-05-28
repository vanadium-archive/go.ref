package lib

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"

	"github.com/gorilla/websocket"
)

// This is basically an io.Writer interface, that allows passing error message
// strings.  This is how the proxy will talk to the javascript/java clients.
type clientWriter interface {
	Write(p []byte) (int, error)

	getLogger() vlog.Logger

	sendError(err error)

	FinishMessage() error
}

// Implements clientWriter interface for sending messages over websockets.
type websocketWriter struct {
	ws     *websocket.Conn
	buf    bytes.Buffer
	logger vlog.Logger // TODO(bprosnitz) Remove this -- it has nothing to do with websockets!
	id     int64
}

func (w *websocketWriter) getLogger() vlog.Logger {
	return w.logger
}

func (w *websocketWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	return len(p), nil
}

func (w *websocketWriter) sendError(err error) {
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

	w.buf.Reset()
	if err := vom.ObjToJSON(&w.buf, vom.ValueOf(response{Type: responseError, Message: errMsg})); err != nil {
		w.logger.VI(2).Info("Failed to marshal with", err)
		return
	}
	if err := w.FinishMessage(); err != nil {
		w.logger.VI(2).Info("WSPR: error finishing message: ", err)
		return
	}
}

func (w *websocketWriter) FinishMessage() error {
	wc, err := w.ws.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	if err := vom.ObjToJSON(wc, vom.ValueOf(websocketMessage{Id: w.id, Data: w.buf.String()})); err != nil {
		return err
	}
	wc.Close()
	w.buf.Reset()
	return nil
}
