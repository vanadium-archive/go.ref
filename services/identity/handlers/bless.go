package handlers

import (
	"fmt"
	"net/http"
	"time"

	"veyron.io/veyron/veyron/services/identity/util"
	"veyron.io/veyron/veyron2/security"
)

// Bless is an http.HandlerFunc that renders/processes a form that takes as
// input a base64-vom-encoded security.PrivateID (blessor) and a
// security.PublicID (blessee) and returns a base64-vom-encoded
// security.PublicID obtained by blessor.Bless(blessee, ...).
func Bless(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderForm(w, r)
		return
	}
	if r.Method != "POST" {
		util.HTTPBadRequest(w, r, fmt.Errorf("Only GET or POST requests accepted"))
		return
	}
	if err := r.ParseForm(); err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("Failed to parse form: %v", err))
		return
	}
	duration, err := time.ParseDuration(defaultIfEmpty(r.FormValue("duration"), "24h"))
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to parse duration: %v", err))
		return
	}
	blessor, err := decodeBlessor(r)
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	blessee, err := decodeBlessee(r)
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	name := r.FormValue("name")
	blessed, err := blessor.Bless(blessee, name, duration, nil)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("%q.Bless(%q, %q) failed: %v", blessor, blessee, name, err))
		return
	}
	util.HTTPSend(w, blessed)
}

func decodeBlessor(r *http.Request) (security.PrivateID, error) {
	var blessor security.PrivateID
	b64 := r.FormValue("blessor")
	if err := util.Base64VomDecode(b64, &blessor); err != nil {
		return nil, fmt.Errorf("Base64VomDecode of blessor into %T failed: %v", blessor, err)
	}
	return blessor, nil
}

func decodeBlessee(r *http.Request) (security.PublicID, error) {
	var pub security.PublicID
	b64 := r.FormValue("blessee")
	if err := util.Base64VomDecode(b64, &pub); err == nil {
		return pub, nil
	}
	var priv security.PrivateID
	err := util.Base64VomDecode(b64, &priv)
	if err == nil {
		return priv.PublicID(), nil
	}
	return nil, fmt.Errorf("Base64VomDecode of blessee into %T or %T failed: %v", pub, priv, err)
}

func renderForm(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`
<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Identity Derivation</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.0.0/css/bootstrap.min.css">
</head>
<body class="container">
<form class="form-signin" method="POST" name="input" action="/bless/">
<h2 class="form-signin-heading">Blessings</h2>
<input type="text" class="form-control" name="blessor" placeholder="Base64VOM encoded PrivateID of blessor">
<br/>
<input type="text" class="form-control" name="blessee" placeholder="Base64VOM encoded PublicID/PrivateID of blessee">
<br/>
<input type="text" class="form-control" name="name" placeholder="Name">
<br/>
<input type="text" class="form-control" name="duration" placeholder="Duration. Defaults to 24h">
<br/>
<button class="btn btn-lg btn-primary btn-block" type="submit">Bless</button>
</form>
</body>
</html>`))
}

func defaultIfEmpty(str, def string) string {
	if len(str) == 0 {
		return def
	}
	return str
}
