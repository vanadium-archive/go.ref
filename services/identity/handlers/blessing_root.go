package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/ref/services/identity/util"
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
	var names []string
	for n, _ := range b.P.BlessingsInfo(b.P.BlessingStore().Default()) {
		names = append(names, n)
	}
	if len(names) == 0 {
		util.HTTPServerError(w, fmt.Errorf("Could not get default blessing name"))
		return
	}

	// TODO(nlacasse,ashankar,ataly): The following line is a HACK. It
	// marshals the public key of the *root* of the blessing chain, rather
	// than the public key of the principal itself.
	//
	// We do this because the identity server is expected to be
	// self-signed, and the javascript tests were breaking when the
	// identity server is run with a blessing like test/child.
	//
	// Once this issue is resolved, delete the following line and uncomment
	// the block below it.
	der, err := rootPublicKey(b.P.BlessingStore().Default())
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	//der, err := b.P.PublicKey().MarshalBinary()
	//if err != nil {
	//	util.HTTPServerError(w, err)
	//	return
	//}
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

// Circuitious route to obtain the certificate chain because the use
// of security.MarshalBlessings is discouraged.
func rootPublicKey(b security.Blessings) ([]byte, error) {
	data, err := vom.Encode(b)
	if err != nil {
		return nil, fmt.Errorf("malformed Blessings: %v", err)
	}
	var wire security.WireBlessings
	if err := vom.Decode(data, &wire); err != nil {
		return nil, fmt.Errorf("malformed WireBlessings: %v", err)
	}
	return wire.CertificateChains[0][0].PublicKey, nil
}
