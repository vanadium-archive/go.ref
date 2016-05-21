// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"v.io/v23/context"
	"v.io/v23/security"
)

const (
	routeRoot      = "/"
	routeHome      = "/home"
	routeCreate    = "/create"
	routeDashboard = "/dashboard"
	routeDestroy   = "/destroy"
	routeOauth     = "/oauth2"
	routeStatic    = "/static/"
	routeStats     = "/stats"
	routeHealth    = "/health"

	paramMessage = "message"
	paramName    = "name"
	// The following parameter names are hardcorded in static/dash.js,
	// and should be changed in tandem.
	paramDashboardName    = "n"
	paramDashbordDuration = "d"

	cookieValidity = 10 * time.Minute
)

type param struct {
	key, value string
}

type params map[string]string

func makeURL(ctx *context.T, baseURL string, p params) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		ctx.Errorf("Parse url error for %v: %v", baseURL, err)
		return ""
	}
	v := url.Values{}
	for param, value := range p {
		v.Add(param, value)
	}
	u.RawQuery = v.Encode()
	return u.String()
}

func replaceParam(ctx *context.T, origURL, param, value string) string {
	u, err := url.Parse(origURL)
	if err != nil {
		ctx.Errorf("Parse url error for %v: %v", origURL, err)
		return ""
	}
	v := u.Query()
	v.Set(param, value)
	u.RawQuery = v.Encode()
	return u.String()
}

type httpArgs struct {
	addr,
	externalURL,
	serverName,
	dashboardGCMMetric,
	dashboardGCMProject,
	monitoringKeyFile string
	secureCookies bool
	oauthCreds    *oauthCredentials
	baseBlessings security.Blessings
	// URI prefix for static assets served from (another) content server.
	staticAssetsPrefix string
	// Manages locally served resources.
	assets *assetsHelper
}

func (a httpArgs) validate() error {
	switch {
	case a.addr == "":
		return errors.New("addr is empty")
	case a.externalURL == "":
		return errors.New("externalURL is empty")
	}
	if err := a.oauthCreds.validate(); err != nil {
		return fmt.Errorf("oauth creds invalid: %v", err)
	}
	return nil
}

// startHTTP is the entry point to the http interface.  It configures and
// launches the http server, and returns a cleanup method to be called at
// shutdown time.
func startHTTP(ctx *context.T, args httpArgs) func() error {
	if err := args.validate(); err != nil {
		ctx.Fatalf("Invalid args %#v: %v", args, err)
	}
	baker := &signedCookieBaker{
		secure:   args.secureCookies,
		signKey:  args.oauthCreds.HashKey,
		validity: cookieValidity,
	}
	// mutating should be true for handlers that mutate state.  For such
	// handlers, any re-authentication should result in redirection to the
	// home page (to foil CSRF attacks that trick the user into launching
	// actions with consequences).
	newHandler := func(f handlerFunc, mutating bool) *handler {
		return &handler{
			ss: &serverState{
				ctx:  ctx,
				args: args,
			},
			baker:    baker,
			f:        f,
			mutating: mutating,
		}
	}

	http.HandleFunc(routeRoot, func(w http.ResponseWriter, r *http.Request) {
		tmplArgs := struct {
			AssetsPrefix,
			Home,
			Email,
			ServerName string
		}{
			AssetsPrefix: args.staticAssetsPrefix,
			Home:         routeHome,
			Email:        "", // Ask the user to log in.
			ServerName:   args.serverName,
		}
		if err := args.assets.executeTemplate(w, rootTmpl, tmplArgs); err != nil {
			args.assets.errorOccurred(ctx, w, r, routeHome, err)
			ctx.Infof("%s[%s] : error %v", r.Method, r.URL, err)
		}
	})
	http.Handle(routeHome, newHandler(handleHome, false))
	http.Handle(routeCreate, newHandler(handleCreate, true))
	http.Handle(routeDashboard, newHandler(handleDashboard, false))
	http.Handle(routeDestroy, newHandler(handleDestroy, true))
	http.HandleFunc(routeOauth, func(w http.ResponseWriter, r *http.Request) {
		handleOauth(ctx, args, baker, w, r)
	})
	http.Handle(routeStatic, http.StripPrefix(routeStatic, args.assets))
	http.Handle(routeStats, newHandler(handleStats, false))
	http.HandleFunc(routeHealth, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ln, err := net.Listen("tcp", args.addr)
	if err != nil {
		ctx.Fatalf("Listen failed: %v", err)
	}
	ctx.Infof("HTTP server at %v [%v]", ln.Addr(), args.externalURL)
	go func() {
		if err := http.Serve(ln, nil); err != nil {
			ctx.Fatalf("Serve failed: %v", err)
		}
	}()
	// NOTE(caprita): closing the listener is necessary but not sufficient
	// for graceful HTTP server shutdown (which should include draining
	// in-flight requests).  See https://github.com/facebookgo/httpdown for
	// example.
	return ln.Close
}

type serverState struct {
	ctx  *context.T
	args httpArgs
}

type requestState struct {
	email, csrfToken string
	w                http.ResponseWriter
	r                *http.Request
}

type handlerFunc func(ss *serverState, rs *requestState) error

// handler wraps handler functions and takes care of providing them with a
// Vanadium context, configuration args, and user's email address (performing
// the oauth flow if the user is not logged in yet).
type handler struct {
	ss       *serverState
	baker    cookieBaker
	f        handlerFunc
	mutating bool
}

// ServeHTTP verifies that the user is logged in, and redirects to the oauth
// flow if not.  If the user is logged in, it extracts the email address from
// the cookie and passes it to the handler function.
func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := h.ss.ctx
	oauthCfg := oauthConfig(h.ss.args.externalURL, h.ss.args.oauthCreds)
	email, csrfToken, err := requireSession(ctx, oauthCfg, h.baker, w, r, h.mutating)
	if err != nil {
		h.ss.args.assets.errorOccurred(ctx, w, r, routeHome, err)
		ctx.Infof("%s[%s] : error %v", r.Method, r.URL, err)
		return
	}
	if email == "" {
		ctx.Infof("%s[%s] -> login", r.Method, r.URL)
		return
	}
	rs := &requestState{
		email:     email,
		csrfToken: csrfToken,
		w:         w,
		r:         r,
	}
	if err := h.f(h.ss, rs); err != nil {
		h.ss.args.assets.errorOccurred(ctx, w, r, makeURL(ctx, routeHome, params{paramCSRF: csrfToken}), err)
		ctx.Infof("%s[%s] : error %v", r.Method, r.URL, err)
		return
	}
	ctx.Infof("%s[%s] : OK", r.Method, r.URL)
}

// The oauth handler is in oauth.go.

func handleHome(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	instances, err := list(ctx, rs.email)
	if err != nil {
		return fmt.Errorf("list error: %v", err)
	}
	type instanceArg struct{ Name, DestroyURL, DashboardURL string }
	tmplArgs := struct {
		AssetsPrefix,
		ServerName,
		Email,
		CreateURL,
		Message string
		Instances []instanceArg
	}{
		AssetsPrefix: ss.args.staticAssetsPrefix,
		ServerName:   ss.args.serverName,
		Email:        rs.email,
		CreateURL:    makeURL(ctx, routeCreate, params{paramCSRF: rs.csrfToken}),
		Message:      rs.r.FormValue(paramMessage),
	}
	for _, instance := range instances {
		tmplArgs.Instances = append(tmplArgs.Instances, instanceArg{
			Name:         instance,
			DestroyURL:   makeURL(ctx, routeDestroy, params{paramName: instance, paramCSRF: rs.csrfToken}),
			DashboardURL: makeURL(ctx, routeDashboard, params{paramDashboardName: relativeMountName(instance), paramCSRF: rs.csrfToken}),
		})
	}
	if err := ss.args.assets.executeTemplate(rs.w, homeTmpl, tmplArgs); err != nil {
		return fmt.Errorf("failed to render home template: %v", err)
	}
	return nil
}

func handleCreate(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	name, err := create(ctx, rs.email, ss.args.baseBlessings)
	if err != nil {
		return fmt.Errorf("create failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "created " + name, paramCSRF: rs.csrfToken})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}

func handleDestroy(ss *serverState, rs *requestState) error {
	ctx := ss.ctx
	name := rs.r.FormValue(paramName)
	if err := destroy(ctx, rs.email, name); err != nil {
		return fmt.Errorf("destroy failed: %v", err)
	}
	redirectTo := makeURL(ctx, routeHome, params{paramMessage: "destroyed " + name, paramCSRF: rs.csrfToken})
	http.Redirect(rs.w, rs.r, redirectTo, http.StatusFound)
	return nil
}
