package handlers

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"veyron.io/veyron/veyron/services/identity/util"
	"veyron.io/veyron/veyron2"
)

// Random is an http.Handler implementation that mints a new Veyron PrivateID
// with a random name.
type Random struct{ Runtime veyron2.Runtime }

func (h Random) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := fmt.Sprintf("random:%d", rand.Intn(1000))
	id, err := h.Runtime.NewIdentity(name)
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	// Bless this with the identity of the runtime, valid for 1 hour.
	blessing, err := h.Runtime.Identity().Bless(id.PublicID(), name, 1*time.Hour, nil)
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	id, err = id.Derive(blessing)
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	util.HTTPSend(w, id)
}
