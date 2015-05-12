// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP server that uses OAuth to create security.Blessings objects.
package server

import (
	"crypto/rand"
	"fmt"
	mrand "math/rand"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/security/audit"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/services/discharger"
	"v.io/x/ref/services/identity/internal/auditor"
	"v.io/x/ref/services/identity/internal/blesser"
	"v.io/x/ref/services/identity/internal/caveats"
	"v.io/x/ref/services/identity/internal/dischargerlib"
	"v.io/x/ref/services/identity/internal/handlers"
	"v.io/x/ref/services/identity/internal/oauth"
	"v.io/x/ref/services/identity/internal/revocation"
	"v.io/x/ref/services/identity/internal/templates"
)

const (
	// TODO(ataly, ashankar, suharshs): The name "google" for the oauthBlesserService does
	// not seem appropriate given our modular construction of the identity server. The
	// oauthBlesserService can use any oauthProvider of its choosing, i.e., it does not
	// always have to be "google". One option would be change the value to "oauth". This
	// would also make the name analogous to that of macaroonService. Note that this option
	// also requires changing the extension.
	oauthBlesserService = "google"
	macaroonService     = "macaroon"
	dischargerService   = "discharger"
)

type IdentityServer struct {
	oauthProvider      oauth.OAuthProvider
	auditor            audit.Auditor
	blessingLogReader  auditor.BlessingLogReader
	revocationManager  revocation.RevocationManager
	oauthBlesserParams blesser.OAuthBlesserParams
	caveatSelector     caveats.CaveatSelector
	rootedObjectAddrs  []naming.Endpoint
	assetsPrefix       string
	mountNamePrefix    string
}

// NewIdentityServer returns a IdentityServer that:
// - uses oauthProvider to authenticate users
// - auditor and blessingLogReader to audit the root principal and read audit logs
// - revocationManager to store revocation data and grant discharges
// - oauthBlesserParams to configure the identity.OAuthBlesser service
func NewIdentityServer(oauthProvider oauth.OAuthProvider, auditor audit.Auditor, blessingLogReader auditor.BlessingLogReader, revocationManager revocation.RevocationManager, oauthBlesserParams blesser.OAuthBlesserParams, caveatSelector caveats.CaveatSelector, assetsPrefix, mountNamePrefix string) *IdentityServer {
	return &IdentityServer{
		oauthProvider:      oauthProvider,
		auditor:            auditor,
		blessingLogReader:  blessingLogReader,
		revocationManager:  revocationManager,
		oauthBlesserParams: oauthBlesserParams,
		caveatSelector:     caveatSelector,
		assetsPrefix:       assetsPrefix,
		mountNamePrefix:    mountNamePrefix,
	}
}

// findUnusedPort finds an unused port and returns it. Of course, no guarantees
// are made that the port will actually be available by the time the caller
// gets around to binding to it. If no port can be found, (0, nil) is returned.
// If an error occurs while creating a socket, that error is returned and the
// other return value is 0.
func findUnusedPort() (int, error) {
	random := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 1000; i++ {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
		if err != nil {
			return 0, err
		}

		port := int(1024 + random.Int31n(64512))
		sa := &syscall.SockaddrInet4{Port: port}
		err = syscall.Bind(fd, sa)
		syscall.Close(fd)
		if err == nil {
			return port, nil
		}
	}

	return 0, nil
}

func (s *IdentityServer) Serve(ctx *context.T, listenSpec *rpc.ListenSpec, externalHttpAddr, httpAddr, tlsConfig string) {
	ctx, err := v23.WithPrincipal(ctx, audit.NewPrincipal(
		v23.GetPrincipal(ctx), s.auditor))
	if err != nil {
		vlog.Panic(err)
	}
	httphost, httpport, err := net.SplitHostPort(httpAddr)
	if err != nil || httpport == "0" {
		httpportNum, err := findUnusedPort()
		if err != nil {
			vlog.Panic(err)
		}
		httpAddr = net.JoinHostPort(httphost, strconv.Itoa(httpportNum))
	}
	rpcServer, _, externalAddr := s.Listen(ctx, listenSpec, externalHttpAddr, httpAddr, tlsConfig)
	fmt.Printf("HTTP_ADDR=%s\n", externalAddr)
	if len(s.rootedObjectAddrs) > 0 {
		fmt.Printf("NAME=%s\n", s.rootedObjectAddrs[0].Name())
	}
	<-signals.ShutdownOnSignals(ctx)
	if err := rpcServer.Stop(); err != nil {
		vlog.Errorf("Failed to stop rpc server: %v", err)
	}
}

func (s *IdentityServer) Listen(ctx *context.T, listenSpec *rpc.ListenSpec, externalHttpAddr, httpAddr, tlsConfig string) (rpc.Server, []string, string) {
	// Setup handlers

	// json-encoded public key and blessing names of this server
	principal := v23.GetPrincipal(ctx)
	http.Handle("/auth/blessing-root", handlers.BlessingRoot{principal})

	macaroonKey := make([]byte, 32)
	if _, err := rand.Read(macaroonKey); err != nil {
		vlog.Fatalf("macaroonKey generation failed: %v", err)
	}

	rpcServer, published, err := s.setupServices(ctx, listenSpec, macaroonKey)
	if err != nil {
		vlog.Fatalf("Failed to setup vanadium services for blessing: %v", err)
	}

	externalHttpAddr = httpAddress(externalHttpAddr, httpAddr)

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	n := "/auth/google/"
	args := oauth.HandlerArgs{
		Principal:               principal,
		MacaroonKey:             macaroonKey,
		Addr:                    fmt.Sprintf("%s%s", externalHttpAddr, n),
		BlessingLogReader:       s.blessingLogReader,
		RevocationManager:       s.revocationManager,
		DischargerLocation:      naming.JoinAddressName(published[0], dischargerService),
		MacaroonBlessingService: naming.JoinAddressName(published[0], macaroonService),
		OAuthProvider:           s.oauthProvider,
		CaveatSelector:          s.caveatSelector,
		AssetsPrefix:            s.assetsPrefix,
	}
	if s.revocationManager != nil {
		args.DischargeServers = appendSuffixTo(published, dischargerService)
	}
	var emptyParams blesser.OAuthBlesserParams
	if !reflect.DeepEqual(s.oauthBlesserParams, emptyParams) {
		args.GoogleServers = appendSuffixTo(published, oauthBlesserService)
	}
	h, err := oauth.NewHandler(args)
	if err != nil {
		vlog.Fatalf("Failed to create HTTP handler for oauth authentication: %v", err)
	}
	http.Handle(n, h)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmplArgs := struct {
			Self                            security.Blessings
			GoogleServers, DischargeServers []string
			ListBlessingsRoute              string
			AssetsPrefix                    string
			Email                           string
		}{
			Self:               principal.BlessingStore().Default(),
			GoogleServers:      args.GoogleServers,
			DischargeServers:   args.DischargeServers,
			ListBlessingsRoute: oauth.ListBlessingsRoute,
			AssetsPrefix:       s.assetsPrefix,
		}
		if err := templates.Home.Execute(w, tmplArgs); err != nil {
			vlog.Info("Failed to render template:", err)
		}
	})
	vlog.Infof("Running HTTP server at: %v", externalHttpAddr)
	go runHTTPSServer(httpAddr, tlsConfig)
	return rpcServer, published, externalHttpAddr
}

func appendSuffixTo(objectname []string, suffix string) []string {
	names := make([]string, len(objectname))
	for i, o := range objectname {
		names[i] = naming.JoinAddressName(o, suffix)
	}
	return names
}

// Starts the blessing services and the discharging service on the same port.
func (s *IdentityServer) setupServices(ctx *context.T, listenSpec *rpc.ListenSpec, macaroonKey []byte) (rpc.Server, []string, error) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new rpc.Server: %v", err)
	}

	principal := v23.GetPrincipal(ctx)
	objectAddr := naming.Join(s.mountNamePrefix, fmt.Sprintf("%v", principal.BlessingStore().Default()))
	if s.rootedObjectAddrs, err = server.Listen(*listenSpec); err != nil {
		defer server.Stop()
		return nil, nil, fmt.Errorf("server.Listen(%v) failed: %v", *listenSpec, err)
	}
	var rootedObjectAddr string
	if naming.Rooted(objectAddr) {
		rootedObjectAddr = objectAddr
	} else if nsroots := v23.GetNamespace(ctx).Roots(); len(nsroots) >= 1 {
		rootedObjectAddr = naming.Join(nsroots[0], objectAddr)
	} else {
		rootedObjectAddr = s.rootedObjectAddrs[0].Name()
	}
	dispatcher := newDispatcher(macaroonKey, oauthBlesserParams(s.oauthBlesserParams, rootedObjectAddr))
	if err := server.ServeDispatcher(objectAddr, dispatcher); err != nil {
		return nil, nil, fmt.Errorf("failed to start Vanadium services: %v", err)
	}
	vlog.Infof("Blessing and discharger services will be published at %v", rootedObjectAddr)
	return server, []string{rootedObjectAddr}, nil
}

// newDispatcher returns a dispatcher for both the blessing and the
// discharging service.
func newDispatcher(macaroonKey []byte, blesserParams blesser.OAuthBlesserParams) rpc.Dispatcher {
	d := dispatcher(map[string]interface{}{
		macaroonService:     blesser.NewMacaroonBlesserServer(macaroonKey),
		dischargerService:   discharger.DischargerServer(dischargerlib.NewDischarger()),
		oauthBlesserService: blesser.NewOAuthBlesserServer(blesserParams),
	})
	// Set up the glob invoker.
	var children []string
	for k, _ := range d {
		children = append(children, k)
	}
	d[""] = rpc.ChildrenGlobberInvoker(children...)
	return d
}

type dispatcher map[string]interface{}

func (d dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if invoker := d[suffix]; invoker != nil {
		return invoker, security.AllowEveryone(), nil
	}
	return nil, nil, verror.New(verror.ErrNoExist, nil, suffix)
}

func oauthBlesserParams(inputParams blesser.OAuthBlesserParams, servername string) blesser.OAuthBlesserParams {
	inputParams.DischargerLocation = naming.Join(servername, dischargerService)
	return inputParams
}

func runHTTPSServer(addr, tlsConfig string) {
	if len(tlsConfig) == 0 {
		vlog.Fatal("Please set the --tls-config flag")
	}
	paths := strings.Split(tlsConfig, ",")
	if len(paths) != 2 {
		vlog.Fatalf("Could not parse --tls-config. Must have exactly two components, separated by a comma")
	}
	vlog.Infof("Starting HTTP server with TLS using certificate [%s] and private key [%s] at https://%s", paths[0], paths[1], addr)
	if err := http.ListenAndServeTLS(addr, paths[0], paths[1], nil); err != nil {
		vlog.Fatalf("http.ListenAndServeTLS failed: %v", err)
	}
}

func httpAddress(externalHttpAddr, httpAddr string) string {
	// If an externalHttpAddr is provided use that.
	if externalHttpAddr != "" {
		httpAddr = externalHttpAddr
	}
	return fmt.Sprintf("https://%v", httpAddr)
}
