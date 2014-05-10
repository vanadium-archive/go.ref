package manager

import (
	"fmt"
	"net/http"

	"veyron2/ipc/stream"
)

// HTTPHandler returns an http.Handler that dumps out debug information from
// the stream.Manager.
//
// If the stream.Manager was not created by InternalNew in this package, an error
// will be returned instead.
//
// TODO(ashankar): This should be made a secure handler that only exposes information
// on VCs that an identity authenticated over HTTPS has access to.
func HTTPHandler(mgr stream.Manager) (http.Handler, error) {
	m, ok := mgr.(*manager)
	if !ok {
		return nil, fmt.Errorf("unrecognized stream.Manager implementation: %T", mgr)
	}
	return httpHandler{m}, nil
}

type httpHandler struct{ m *manager }

func (h httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(h.m.DebugString()))
}
