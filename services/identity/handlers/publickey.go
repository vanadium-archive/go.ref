package handlers

import (
	"fmt"
	"net/http"

	"veyron.io/veyron/veyron/services/identity/util"
	"veyron.io/veyron/veyron2/security"
)

// PublicKey is an http.Handler implementation that renders a public key in
// DER format.
type PublicKey struct{ P security.PublicID }

func (h PublicKey) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	der, err := h.P.PublicKey().MarshalBinary()
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%v.der", h.P))
	w.Write(der)
}
