package handlers

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"veyron/services/identity/revocation"

	"veyron2/security"
)

// Revoke is an http.Handler implementation that revokes a Veyron PrivateID.
type Revoke struct {
	RevocationManager *revocation.RevocationManager
}

// TODO(suharshs): Move this to the googleoauth handler to enable authorization on this.
func (h Revoke) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	const (
		success = `{"success": "true"}`
		failure = `{"success": "false"}`
	)
	// Get the caveat string from the request.
	// TODO(suharshs): Send a multi part form value from the client to server and parse it here.
	content, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("Failed to parse request: %s", err)
		w.Write([]byte(failure))
		return
	}
	decoded_caveatID, err := base64.URLEncoding.DecodeString(string(content))
	if err != nil {
		fmt.Printf("base64 decoding failed: %s", err)
		w.Write([]byte(failure))
		return
	}

	caveatID := security.ThirdPartyCaveatID(string(decoded_caveatID))

	if err := h.RevocationManager.Revoke(caveatID); err != nil {
		fmt.Printf("Revocation failed: %s", err)
		w.Write([]byte(failure))
		return
	}

	w.Write([]byte(success))
	return
}
