package handlers

import (
	"fmt"
	"math/rand"
	"net/http"

	"veyron/services/identity/util"
	"veyron2"
)

// Random is an http.Handler implementation that mints a new Veyron PrivateID
// with a random name.
type Random struct{ Runtime veyron2.Runtime }

func (h Random) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := h.Runtime.NewIdentity(fmt.Sprintf("random:%d", rand.Intn(1000)))
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	util.HTTPSend(w, id)
}
