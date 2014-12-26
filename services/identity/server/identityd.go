// HTTP server that uses OAuth to create security.Blessings objects.
package server

import (
	"crypto/rand"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/security/audit"
	"v.io/core/veyron/services/identity/auditor"
	"v.io/core/veyron/services/identity/blesser"
	"v.io/core/veyron/services/identity/caveats"
	"v.io/core/veyron/services/identity/handlers"
	"v.io/core/veyron/services/identity/oauth"
	"v.io/core/veyron/services/identity/revocation"
	services "v.io/core/veyron/services/security"
	"v.io/core/veyron/services/security/discharger"

	"v.io/core/veyron/profiles/static"
)

var (
	// Flags controlling the HTTP server
	httpaddr  = flag.String("httpaddr", "localhost:8125", "Address on which the HTTP server listens on.")
	tlsconfig = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. This must be provided.")
	host      = flag.String("host", defaultHost(), "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the root name and public key of the signer at 'x.com/blessing-root'.")
)

const (
	oauthBlesserService = "google"
	macaroonService     = "macaroon"
	dischargerService   = "discharger"
)

type identityd struct {
	oauthProvider      oauth.OAuthProvider
	auditor            audit.Auditor
	blessingLogReader  auditor.BlessingLogReader
	revocationManager  revocation.RevocationManager
	oauthBlesserParams blesser.GoogleParams
	caveatSelector     caveats.CaveatSelector
}

// NewIdentityServer returns a IdentityServer that:
// - uses oauthProvider to authenticate users
// - auditor and blessingLogReader to audit the root principal and read audit logs
// - revocationManager to store revocation data and grant discharges
// - oauthBlesserParams to configure the identity.OAuthBlesser service
func NewIdentityServer(oauthProvider oauth.OAuthProvider, auditor audit.Auditor, blessingLogReader auditor.BlessingLogReader, revocationManager revocation.RevocationManager, oauthBlesserParams blesser.GoogleParams, caveatSelector caveats.CaveatSelector) *identityd {
	return &identityd{
		oauthProvider,
		auditor,
		blessingLogReader,
		revocationManager,
		oauthBlesserParams,
		caveatSelector,
	}
}

func (s *identityd) Serve() {
	flag.Parse()

	runtime, err := rt.New(options.RuntimePrincipal{providerPrincipal(s.auditor)})
	if err != nil {
		vlog.Fatal(err)
	}
	defer runtime.Cleanup()

	// Setup handlers
	http.Handle("/blessing-root", handlers.BlessingRoot{runtime.Principal()}) // json-encoded public key and blessing names of this server

	macaroonKey := make([]byte, 32)
	if _, err := rand.Read(macaroonKey); err != nil {
		vlog.Fatalf("macaroonKey generation failed: %v", err)
	}

	ipcServer, published, err := s.setupServices(runtime, macaroonKey)
	if err != nil {
		vlog.Fatalf("Failed to setup veyron services for blessing: %v", err)
	}
	defer ipcServer.Stop()

	n := "/google/"
	h, err := oauth.NewHandler(oauth.HandlerArgs{
		R:                       runtime,
		MacaroonKey:             macaroonKey,
		Addr:                    fmt.Sprintf("%s%s", httpaddress(), n),
		BlessingLogReader:       s.blessingLogReader,
		RevocationManager:       s.revocationManager,
		DischargerLocation:      naming.JoinAddressName(published[0], dischargerService),
		MacaroonBlessingService: naming.JoinAddressName(published[0], macaroonService),
		OAuthProvider:           s.oauthProvider,
		CaveatSelector:          s.caveatSelector,
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
			Self: runtime.Principal().BlessingStore().Default(),
		}
		if s.revocationManager != nil {
			args.DischargeServers = appendSuffixTo(published, dischargerService)
		}
		var emptyParams blesser.GoogleParams
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
	vlog.Infof("Running HTTP server at: %v", httpaddress())
	go runHTTPSServer(*httpaddr)
	<-signals.ShutdownOnSignals(runtime)
}

func appendSuffixTo(objectname []string, suffix string) []string {
	names := make([]string, len(objectname))
	for i, o := range objectname {
		names[i] = naming.JoinAddressName(o, suffix)
	}
	return names
}

// Starts the blessing services and the discharging service on the same port.
func (s *identityd) setupServices(r veyron2.Runtime, macaroonKey []byte) (ipc.Server, []string, error) {
	server, err := r.NewServer()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new ipc.Server: %v", err)
	}
	eps, err := server.Listen(static.ListenSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("server.Listen(%v) failed: %v", static.ListenSpec, err)
	}
	ep := eps[0]

	dispatcher := newDispatcher(macaroonKey, oauthBlesserParams(s.oauthBlesserParams, s.revocationManager, ep))
	objectname := naming.Join("identity", fmt.Sprintf("%v", r.Principal().BlessingStore().Default()))
	if err := server.ServeDispatcher(objectname, dispatcher); err != nil {
		return nil, nil, fmt.Errorf("failed to start Veyron services: %v", err)
	}
	vlog.Infof("Blessing and discharger services enabled at %v", naming.JoinAddressName(ep.String(), objectname))
	published, _ := server.Published()
	if len(published) == 0 {
		// No addresses published, publish the endpoint instead (which may not be usable everywhere, but oh-well).
		published = append(published, ep.String())
	}
	return server, published, nil
}

// newDispatcher returns a dispatcher for both the blessing and the
// discharging service.
func newDispatcher(macaroonKey []byte, blesserParams blesser.GoogleParams) ipc.Dispatcher {
	d := dispatcher(map[string]interface{}{
		macaroonService:     blesser.NewMacaroonBlesserServer(macaroonKey),
		dischargerService:   services.DischargerServer(discharger.NewDischarger()),
		oauthBlesserService: blesser.NewGoogleOAuthBlesserServer(blesserParams),
	})
	return d
}

type allowEveryoneAuthorizer struct{}

func (allowEveryoneAuthorizer) Authorize(security.Context) error { return nil }

type dispatcher map[string]interface{}

func (d dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if invoker := d[suffix]; invoker != nil {
		return invoker, allowEveryoneAuthorizer{}, nil
	}
	return nil, nil, verror.Make(verror.NoExist, nil, suffix)
}

func oauthBlesserParams(inputParams blesser.GoogleParams, revocationManager revocation.RevocationManager, ep naming.Endpoint) blesser.GoogleParams {
	inputParams.DischargerLocation = naming.JoinAddressName(ep.String(), dischargerService)
	return inputParams
}

func runHTTPSServer(addr string) {
	if len(*tlsconfig) == 0 {
		vlog.Fatal("Please set the --tlsconfig flag")
	}
	paths := strings.Split(*tlsconfig, ",")
	if len(paths) != 2 {
		vlog.Fatalf("Could not parse --tlsconfig. Must have exactly two components, separated by a comma")
	}
	vlog.Infof("Starting HTTP server with TLS using certificate [%s] and private key [%s] at https://%s", paths[0], paths[1], addr)
	if err := http.ListenAndServeTLS(addr, paths[0], paths[1], nil); err != nil {
		vlog.Fatalf("http.ListenAndServeTLS failed: %v", err)
	}
}

func defaultHost() string {
	host, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("Failed to get hostname: %v", err)
	}
	return host
}

// providerPrincipal returns the Principal to use for the identity provider (i.e., this program).
func providerPrincipal(auditor audit.Auditor) security.Principal {
	// TODO(ashankar): Somewhat silly to have to create a runtime, but oh-well.
	r, err := rt.New()
	if err != nil {
		vlog.Fatal(err)
	}
	defer r.Cleanup()
	return audit.NewPrincipal(r.Principal(), auditor)
}

func httpaddress() string {
	_, port, err := net.SplitHostPort(*httpaddr)
	if err != nil {
		vlog.Fatalf("Failed to parse %q: %v", *httpaddr, err)
	}
	return fmt.Sprintf("https://%s:%v", *host, port)
}

var tmpl = template.Must(template.New("main").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Identity Server</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
</head>
<body>
<div class="container">
<div class="page-header"><h2>{{.Self}}</h2><h4>A Veyron Blessing Provider</h4></div>
<div class="well">
This is a Veyron identity provider that provides blessings with the name prefix <mark>{{.Self}}</mark>.
<br/>
The public key of this provider is {{.Self.PublicKey}}.
<br/>
The root names and public key (in DER encoded <a href="http://en.wikipedia.org/wiki/X.690#DER_encoding">format</a>)
are available in a <a class="btn btn-xs btn-primary" href="/blessing-root">JSON</a> object.
</div>

<div class="well">
<ul>
{{if .GoogleServers}}
<li>Blessings (using Google OAuth to fetch an email address) are provided via Veyron RPCs to: <tt>{{range .GoogleServers}}{{.}}{{end}}</tt></li>
{{end}}
{{if .DischargeServers}}
<li>RevocationCaveat Discharges are provided via Veyron RPCs to: <tt>{{range .DischargeServers}}{{.}}{{end}}</tt></li>
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
