// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/ref/services/identity/internal/util"
)

// BlessingRoot is an http.Handler implementation that renders the server's
// blessing names and public key in a json string.
type BlessingRoot struct {
	P security.Principal
}

// Cached response so we don't have to bless and encode every time somebody
// hits this route.
var cachedResponseJson []byte

func (b BlessingRoot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if cachedResponseJson != nil {
		respondJson(w, cachedResponseJson)
		return
	}

	// The identity service itself is blessed by a more protected key.
	// Use the root certificate as the identity provider.
	//
	// TODO(ashankar): This is making the assumption that the identity
	// service has a single blessing, which may not be true in general.
	// Revisit this.
	name, der, err := rootCertificateDetails(b.P.BlessingStore().Default())
	if err != nil {
		util.HTTPServerError(w, err)
		return
	}
	str := base64.URLEncoding.EncodeToString(der)

	// TODO(suharshs): Ideally this struct would be BlessingRootResponse but vdl does
	// not currently allow field annotations. Once those are allowed, then use that
	// here.
	rootInfo := struct {
		Names     []string `json:"names"`
		PublicKey string   `json:"publicKey"`
	}{
		Names:     []string{name},
		PublicKey: str,
	}

	res, err := json.Marshal(rootInfo)
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
func rootCertificateDetails(b security.Blessings) (string, []byte, error) {
	data, err := vom.Encode(b)
	if err != nil {
		return "", nil, fmt.Errorf("malformed Blessings: %v", err)
	}
	var wire security.WireBlessings
	if err := vom.Decode(data, &wire); err != nil {
		return "", nil, fmt.Errorf("malformed WireBlessings: %v", err)
	}
	cert := wire.CertificateChains[0][0]
	return cert.Extension, cert.PublicKey, nil
}
