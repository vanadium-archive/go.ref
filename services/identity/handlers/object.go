package handlers

import (
	"net/http"

	"veyron/services/identity/util"
)

// Object implements an http.Handler that writes out the provided object in the
// HTTP response after base64 encoding the Vom-encoded object.
type Object struct{ Object interface{} }

func (h Object) ServeHTTP(w http.ResponseWriter, r *http.Request) { util.HTTPSend(w, h.Object) }
