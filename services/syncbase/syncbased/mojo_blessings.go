// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

// The evolving story of identification and security in syncbase
// running in mojo:
// (Last Updated: November 5, 2015).
//
// SHORT STORY:
// The syncbase mojo service gets a blessing by exchanging an OAuth2 token for
// a blessing. This allows syncbases to identify each other when synchronizing
// data between each other and enables basic security policies like "all my
// devices sync all my data with each other". However, different mojo "clients"
// (that communicate with the local syncbase via mojo IPC) are not isolated
// from each other - so each mojo app has complete access to all the data
// written to the local syncbase service (modulo any filesystem isolation via
// the --root-dir flag). This is something that we intend to change.
//
// LONGER VERSION:
// A "mojo app" consists of a set of interrelated components, accessed by a
// URL. Communication between these components is via mojo IPC. One of those
// components is syncbase (this package), which is a store that synchronizes
// between devices via Vanadium RPCs. See:
// https://docs.google.com/a/google.com/document/d/1TyxPYIhj9VBCtY7eAXu_MEV9y0dtRx7n7UY4jm76Qq4/edit?usp=sharing
//
// Any process communicating via Vanadium RPCs needs to have an identity
// represented as a Blessing
// (https://github.com/vanadium/docs/blob/master/concepts/security.md).
//
// So, the current story is this:
// 1 The mojo syncbase app/service fetches an OAuth2 token from the
//   mojo authentication service
// 2 It exchanges this token for a blessing bound to the public
//   key of the principal associated with this service
//   (this is essentially a copy of what the mojo principal service
//   implementation does)
// 3 And all RPCs use the blessing obtained above
// 4 Other mojo components communicate with their local syncbase via
//   Mojo IPC (see v.io/x/ref/services/syncbase/server/mojo_call.go)
//   and DO NOT AUTHENTICATE with it
// 5 The resulting security model is that all mojo apps on the same
//   device have access to whatever is stored in the local syncbase
//
// Clearly (5) is not ideal. And as this evolves, the plan is likely to be
// something involving each "client" of this syncbase mojo service being
// identified using the principal service
// (https://github.com/domokit/mojo/blob/master/mojo/services/vanadium/security/interfaces/principal.mojom).
// But more on that when we get there.

package main

import (
	"encoding/base64"
	"fmt"
	"mojo/public/go/application"
	"mojo/public/go/bindings"
	"mojo/public/go/system"
	"mojo/public/interfaces/network/url_request"
	"mojo/services/authentication/interfaces/authentication"
	"mojo/services/network/interfaces/network_service"
	"mojo/services/network/interfaces/url_loader"
	"net/url"
	"runtime"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/vom"
	seclib "v.io/x/ref/lib/security"
)

func setBlessings(v23ctx *context.T, appctx application.Context) error {
	// Get an OAuth2 token from the mojo authentication service.
	// At the time of this writing, the mojo authentication
	// service was implemented only for Android, so in absence
	// of that abort. Which means that the syncbases cannot
	// communicate with each other, unless --v23.credentials was
	// supplied as a flag.
	if runtime.GOOS != "android" {
		v23ctx.Infof("Using default blessings for non-Android OS (%v)", runtime.GOOS)
		return nil
	}
	// TODO(ashankar,ataly): This is almost a duplicate of what
	// is in
	// https://github.com/domokit/mojo/blob/master/services/vanadium/security/principal_service.go
	// - at some point this needs to be cleared up and this
	// syncbase should use the principal service?
	token, err := oauthToken(appctx)
	if err != nil {
		return err
	}
	v23ctx.Infof("Obtained OAuth2 token, will exchange for blessings")
	p := v23.GetPrincipal(v23ctx)
	blessings, err := token2blessings(appctx, token, p.PublicKey())
	if err != nil {
		return err
	}
	v23ctx.Infof("Obtained blessings %v", blessings)
	if err := seclib.SetDefaultBlessings(p, blessings); err != nil {
		return fmt.Errorf("failed to use blessings: %v", err)
	}
	return nil
}

func oauthToken(ctx application.Context) (string, error) {
	req, ptr := authentication.CreateMessagePipeForAuthenticationService()
	ctx.ConnectToApplication("mojo:authentication").ConnectToService(&req)
	proxy := authentication.NewAuthenticationServiceProxy(ptr, bindings.GetAsyncWaiter())
	name, errstr, _ := proxy.SelectAccount(true /*return last selected*/)
	if name == nil || errstr != nil {
		return "", fmt.Errorf("failed to select an account for user: %v", errstr)
	}
	token, errstr, _ := proxy.GetOAuth2Token(*name, []string{"email"})
	if token == nil || errstr != nil {
		return "", fmt.Errorf("failed to obtain an OAuth2 token for %q: %v", *name, errstr)
	}
	return *token, nil
}

func token2blessings(ctx application.Context, token string, key security.PublicKey) (security.Blessings, error) {
	var ret security.Blessings
	netReq, netPtr := network_service.CreateMessagePipeForNetworkService()
	ctx.ConnectToApplication("mojo:network_service").ConnectToService(&netReq)
	netProxy := network_service.NewNetworkServiceProxy(netPtr, bindings.GetAsyncWaiter())

	loaderReq, loaderPtr := url_loader.CreateMessagePipeForUrlLoader()
	if err := netProxy.CreateUrlLoader(loaderReq); err != nil {
		return ret, fmt.Errorf("failed to create URL loader: %v", err)
	}
	url, err := token2blessingURL(token, key)
	if err != nil {
		return ret, err
	}
	response, err := url_loader.NewUrlLoaderProxy(loaderPtr, bindings.GetAsyncWaiter()).Start(url_request.UrlRequest{
		Url:    url,
		Method: "GET",
	})
	if err != nil || response.Error != nil {
		var errrep interface{}
		if err != nil {
			errrep = err
		} else {
			errrep = response.Error
		}
		return ret, fmt.Errorf("HTTP request to exchange OAuth token for blessings failed: %v", errrep)
	}
	res, b64bytes := (*response.Body).ReadData(system.MOJO_READ_DATA_FLAG_ALL_OR_NONE)
	if res != system.MOJO_RESULT_OK {
		return ret, fmt.Errorf("failed to read blessings from the Vanadium identity provider. Result: %v", res)
	}
	vombytes, err := base64.URLEncoding.DecodeString(string(b64bytes))
	if err != nil {
		return ret, fmt.Errorf("invalid base64 encoded blessings: %v", err)
	}
	if err := vom.Decode(vombytes, &ret); err != nil {
		return ret, fmt.Errorf("invalid encoded blessings: %v", err)
	}
	return ret, nil
}

func token2blessingURL(token string, key security.PublicKey) (string, error) {
	base, err := url.Parse("https://dev.v.io/auth/google/bless")
	if err != nil {
		return "", fmt.Errorf("failed to parse blessing URL: %v", err)
	}
	pub, err := key.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("invalid public key: %v", err)
	}

	var params url.Values
	params.Add("public_key", base64.URLEncoding.EncodeToString(pub))
	params.Add("token", token)
	params.Add("output_format", "base64vom")

	base.RawQuery = params.Encode()
	return base.String(), nil
}
