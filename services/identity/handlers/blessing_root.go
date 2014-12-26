package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"v.io/core/veyron/services/identity/util"
	"v.io/core/veyron2/security"
)

// BlessingRoot is an http.Handler implementation that renders the server's
// blessing names and public key in a json string.
type BlessingRoot struct {
	P security.Principal
}

type BlessingRootResponse struct {
	// Names of the blessings.
	Names []string `json:"names"`
	// Base64 der-encoded public key.
	PublicKey string `json:"publicKey"`
}

// Cached response so we don't have to bless and encode every time somebody
// hits this route.
var cachedResponseJson []byte

func (b BlessingRoot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if cachedResponseJson != nil {
		respondJson(w, cachedResponseJson)
		return
	}

	// Get the blessing names of the local principal.
	// TODO(nlacasse,ataly,gauthamt): Make this easier to do. For now we
	// have to bless a context with the same LocalPrincipal as ours.
	ctx := security.NewContext(&security.ContextParams{
		LocalPrincipal: b.P,
	})
	names := b.P.BlessingStore().Default().ForContext(ctx)

	if len(names) == 0 {
		util.HTTPServerError(w, fmt.Errorf("Could not get blessing name."))
		return
	}

	der, err := b.P.PublicKey().MarshalBinary()
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	str := base64.URLEncoding.EncodeToString(der)

	res, err := json.Marshal(BlessingRootResponse{
		Names:     names,
		PublicKey: str,
	})
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}

	cachedResponseJson = res
	respondJson(w, res)
}

func respondJson(w http.ResponseWriter, res []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}
