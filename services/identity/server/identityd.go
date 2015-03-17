// HTTP server that uses OAuth to create security.Blessings objects.
package server

import (
	"crypto/rand"
	"fmt"
	"html/template"
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
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/signals"
	"v.io/x/ref/security/audit"
	"v.io/x/ref/services/identity/auditor"
	"v.io/x/ref/services/identity/blesser"
	"v.io/x/ref/services/identity/caveats"
	"v.io/x/ref/services/identity/handlers"
	"v.io/x/ref/services/identity/oauth"
	"v.io/x/ref/services/identity/revocation"
	"v.io/x/ref/services/identity/util"
	services "v.io/x/ref/services/security"
	"v.io/x/ref/services/security/discharger"
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
	emailClassifier    *util.EmailClassifier
	rootedObjectAddrs  []naming.Endpoint
}

// NewIdentityServer returns a IdentityServer that:
// - uses oauthProvider to authenticate users
// - auditor and blessingLogReader to audit the root principal and read audit logs
// - revocationManager to store revocation data and grant discharges
// - oauthBlesserParams to configure the identity.OAuthBlesser service
func NewIdentityServer(oauthProvider oauth.OAuthProvider, auditor audit.Auditor, blessingLogReader auditor.BlessingLogReader, revocationManager revocation.RevocationManager, oauthBlesserParams blesser.OAuthBlesserParams, caveatSelector caveats.CaveatSelector, emailClassifier *util.EmailClassifier) *IdentityServer {
	return &IdentityServer{
		oauthProvider,
		auditor,
		blessingLogReader,
		revocationManager,
		oauthBlesserParams,
		caveatSelector,
		emailClassifier,
		nil,
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

func (s *IdentityServer) Serve(ctx *context.T, listenSpec *ipc.ListenSpec, host, httpaddr, tlsconfig string) {
	ctx, err := v23.SetPrincipal(ctx, audit.NewPrincipal(
		v23.GetPrincipal(ctx), s.auditor))
	if err != nil {
		vlog.Panic(err)
	}
	httphost, httpport, err := net.SplitHostPort(httpaddr)
	if err != nil || httpport == "0" {
		httpportNum, err := findUnusedPort()
		if err != nil {
			vlog.Panic(err)
		}
		httpaddr = net.JoinHostPort(httphost, strconv.Itoa(httpportNum))
	}
	ipcServer, _, externalAddr := s.Listen(ctx, listenSpec, host, httpaddr, tlsconfig)
	fmt.Printf("HTTP_ADDR=%s\n", externalAddr)
	if len(s.rootedObjectAddrs) > 0 {
		fmt.Printf("NAME=%s\n", s.rootedObjectAddrs[0].Name())
	}
	<-signals.ShutdownOnSignals(ctx)
	if err := ipcServer.Stop(); err != nil {
		vlog.Errorf("Failed to stop ipc server: %v", err)
	}
}

func (s *IdentityServer) Listen(ctx *context.T, listenSpec *ipc.ListenSpec, host, httpaddr, tlsconfig string) (ipc.Server, []string, string) {
	// Setup handlers

	// json-encoded public key and blessing names of this server
	principal := v23.GetPrincipal(ctx)
	http.Handle("/blessing-root", handlers.BlessingRoot{principal})

	macaroonKey := make([]byte, 32)
	if _, err := rand.Read(macaroonKey); err != nil {
		vlog.Fatalf("macaroonKey generation failed: %v", err)
	}

	ipcServer, published, err := s.setupServices(ctx, listenSpec, macaroonKey)
	if err != nil {
		vlog.Fatalf("Failed to setup vanadium services for blessing: %v", err)
	}

	externalHttpaddr := httpaddress(host, httpaddr)

	n := "/google/"
	h, err := oauth.NewHandler(oauth.HandlerArgs{
		Principal:               principal,
		MacaroonKey:             macaroonKey,
		Addr:                    fmt.Sprintf("%s%s", externalHttpaddr, n),
		BlessingLogReader:       s.blessingLogReader,
		RevocationManager:       s.revocationManager,
		DischargerLocation:      naming.JoinAddressName(published[0], dischargerService),
		MacaroonBlessingService: naming.JoinAddressName(published[0], macaroonService),
		OAuthProvider:           s.oauthProvider,
		CaveatSelector:          s.caveatSelector,
		EmailClassifier:         s.emailClassifier,
	})
	if err != nil {
		vlog.Fatalf("Failed to create HTTP handler for oauth authentication: %v", err)
	}
	http.Handle(n, h)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		args := struct {
			Self                            security.Blessings
			GoogleServers, DischargeServers []string
			ListBlessingsRoute              string
		}{
			Self: principal.BlessingStore().Default(),
		}
		if s.revocationManager != nil {
			args.DischargeServers = appendSuffixTo(published, dischargerService)
		}
		var emptyParams blesser.OAuthBlesserParams
		if !reflect.DeepEqual(s.oauthBlesserParams, emptyParams) {
			args.GoogleServers = appendSuffixTo(published, oauthBlesserService)
		}
		if s.blessingLogReader != nil {
			args.ListBlessingsRoute = oauth.ListBlessingsRoute
		}
		if err := tmpl.Execute(w, args); err != nil {
			vlog.Info("Failed to render template:", err)
		}
	})
	vlog.Infof("Running HTTP server at: %v", externalHttpaddr)
	go runHTTPSServer(httpaddr, tlsconfig)
	return ipcServer, published, externalHttpaddr
}

func appendSuffixTo(objectname []string, suffix string) []string {
	names := make([]string, len(objectname))
	for i, o := range objectname {
		names[i] = naming.JoinAddressName(o, suffix)
	}
	return names
}

// Starts the blessing services and the discharging service on the same port.
func (s *IdentityServer) setupServices(ctx *context.T, listenSpec *ipc.ListenSpec, macaroonKey []byte) (ipc.Server, []string, error) {
	server, err := v23.NewServer(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new ipc.Server: %v", err)
	}

	principal := v23.GetPrincipal(ctx)
	objectAddr := naming.Join("identity", fmt.Sprintf("%v", principal.BlessingStore().Default()))
	var rootedObjectAddr string
	if eps, err := server.Listen(*listenSpec); err != nil {
		defer server.Stop()
		return nil, nil, fmt.Errorf("server.Listen(%v) failed: %v", *listenSpec, err)
	} else if nsroots := v23.GetNamespace(ctx).Roots(); len(nsroots) >= 1 {
		rootedObjectAddr = naming.Join(nsroots[0], objectAddr)
		s.rootedObjectAddrs = eps
	} else {
		rootedObjectAddr = eps[0].Name()
		s.rootedObjectAddrs = eps
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
func newDispatcher(macaroonKey []byte, blesserParams blesser.OAuthBlesserParams) ipc.Dispatcher {
	d := dispatcher(map[string]interface{}{
		macaroonService:     blesser.NewMacaroonBlesserServer(macaroonKey),
		dischargerService:   services.DischargerServer(discharger.NewDischarger()),
		oauthBlesserService: blesser.NewOAuthBlesserServer(blesserParams),
	})
	// Set up the glob invoker.
	var children []string
	for k, _ := range d {
		children = append(children, k)
	}
	d[""] = ipc.ChildrenGlobberInvoker(children...)
	return d
}

type allowEveryoneAuthorizer struct{}

func (allowEveryoneAuthorizer) Authorize(*context.T) error { return nil }

type dispatcher map[string]interface{}

func (d dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if invoker := d[suffix]; invoker != nil {
		return invoker, allowEveryoneAuthorizer{}, nil
	}
	return nil, nil, verror.New(verror.ErrNoExist, nil, suffix)
}

func oauthBlesserParams(inputParams blesser.OAuthBlesserParams, servername string) blesser.OAuthBlesserParams {
	inputParams.DischargerLocation = naming.Join(servername, dischargerService)
	return inputParams
}

func runHTTPSServer(addr, tlsconfig string) {
	if len(tlsconfig) == 0 {
		vlog.Fatal("Please set the --tlsconfig flag")
	}
	paths := strings.Split(tlsconfig, ",")
	if len(paths) != 2 {
		vlog.Fatalf("Could not parse --tlsconfig. Must have exactly two components, separated by a comma")
	}
	vlog.Infof("Starting HTTP server with TLS using certificate [%s] and private key [%s] at https://%s", paths[0], paths[1], addr)
	if err := http.ListenAndServeTLS(addr, paths[0], paths[1], nil); err != nil {
		vlog.Fatalf("http.ListenAndServeTLS failed: %v", err)
	}
}

func httpaddress(host, httpaddr string) string {
	_, port, err := net.SplitHostPort(httpaddr)
	if err != nil {
		vlog.Fatalf("Failed to parse %q: %v", httpaddr, err)
	}
	return fmt.Sprintf("https://%s:%v", host, port)
}

var tmpl = template.Must(template.New("main").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Vanadium Identity Server</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
</head>
<body>
<div class="container">
<div class="page-header"><h2>{{.Self}}</h2><h4>A Vanadium Blessing Provider</h4></div>
<div class="well">
This is a Vanadium identity provider that provides blessings with the name prefix <mark>{{.Self}}</mark>.
<br/>
The public key of this provider is {{.Self.PublicKey}}.
<br/>
The root names and public key (in DER encoded <a href="http://en.wikipedia.org/wiki/X.690#DER_encoding">format</a>)
are available in a <a class="btn btn-xs btn-primary" href="/blessing-root">JSON</a> object.
</div>

<div class="well">
<ul>
{{if .GoogleServers}}
<li>Blessings (using Google OAuth to fetch an email address) are provided via Vanadium RPCs to: <tt>{{range .GoogleServers}}{{.}}{{end}}</tt></li>
{{end}}
{{if .DischargeServers}}
<li>RevocationCaveat Discharges are provided via Vanadium RPCs to: <tt>{{range .DischargeServers}}{{.}}{{end}}</tt></li>
{{end}}
{{if .ListBlessingsRoute}}
<li>You can <a class="btn btn-xs btn-primary" href="/google/{{.ListBlessingsRoute}}">enumerate</a> blessings provided with your
email address.</li>
{{end}}
</ul>
</div>

</div>
</body>
</html>`))
