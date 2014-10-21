// HTTP server that uses OAuth to create security.PrivateID objects.
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/audit"
	"veyron.io/veyron/veyron/services/identity/auditor"
	"veyron.io/veyron/veyron/services/identity/blesser"
	"veyron.io/veyron/veyron/services/identity/googleoauth"
	"veyron.io/veyron/veyron/services/identity/handlers"
	"veyron.io/veyron/veyron/services/identity/revocation"
	services "veyron.io/veyron/veyron/services/security"
	"veyron.io/veyron/veyron/services/security/discharger"

	"veyron.io/veyron/veyron/profiles/static"
	_ "veyron.io/veyron/veyron/runtimes/google/security"
)

var (
	httpaddr      = flag.String("httpaddr", "localhost:8125", "Address on which the HTTP server listens on.")
	tlsconfig     = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. If empty, will not use HTTPS.")
	host          = flag.String("host", defaultHost(), "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the public key of the signer at 'x.com/pubkey/'.")
	minExpiryDays = flag.Int("min_expiry_days", 365, "Minimum expiry time (in days) of identities issued by this server")

	auditprefix = flag.String("audit", "", "File prefix to files where auditing information will be written.")
	auditfilter = flag.String("audit_filter", "", "If non-empty, instead of starting the server the audit log will be dumped to STDOUT (with the filter set to the value of this flag. '/' can be used to dump all events).")

	// Configuration for various Google OAuth-based clients.
	googleConfigWeb     = flag.String("google_config_web", "", "Path to JSON-encoded OAuth client configuration for the web application that renders the audit log for blessings provided by this provider.")
	googleConfigChrome  = flag.String("google_config_chrome", "", "Path to the JSON-encoded OAuth client configuration for Chrome browser applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleConfigAndroid = flag.String("google_config_android", "", "Path to the JSON-encoded OAuth client configuration for Android applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleDomain        = flag.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")

	// Revoker/Discharger configuration
	// TODO(ashankar,ataly,suharshs): Re-enable by default once the move to the new security API is complete?
	revocationDir = flag.String("revocation_dir", "" /*filepath.Join(os.TempDir(), "revocation_dir")*/, "Path where the revocation manager will store caveat and revocation information.")
)

const (
	googleService     = "google"
	macaroonService   = "macaroon"
	dischargerService = "discharger"
)

func main() {
	flag.Usage = usage
	r := rt.Init(providerIdentity())
	defer r.Cleanup()

	if len(*auditfilter) > 0 {
		dumpAuditLog()
		return
	}

	// Calling with empty string returns a empty RevocationManager
	revocationManager, err := revocation.NewRevocationManager(*revocationDir)
	if err != nil {
		vlog.Fatalf("Failed to start RevocationManager: %v", err)
	}

	// Setup handlers
	http.Handle("/pubkey/", handlers.PublicKey{r.Identity().PublicID()}) // public key of this server
	if enableRandomHandler() {
		http.Handle("/random/", handlers.Random{r}) // mint identities with a random name
	}
	macaroonKey := make([]byte, 32)
	if _, err := rand.Read(macaroonKey); err != nil {
		vlog.Fatalf("macaroonKey generation failed: %v", err)
	}
	// Google OAuth
	ipcServer, published, err := setupServices(r, revocationManager, macaroonKey)
	if err != nil {
		vlog.Fatalf("Failed to setup veyron services for blessing: %v", err)
	}
	if ipcServer != nil {
		defer ipcServer.Stop()
	}
	if clientID, clientSecret, ok := getOAuthClientIDAndSecret(*googleConfigWeb); ok {
		n := "/google/"
		h, err := googleoauth.NewHandler(googleoauth.HandlerArgs{
			R:                       r,
			MacaroonKey:             macaroonKey,
			Addr:                    fmt.Sprintf("%s%s", httpaddress(), n),
			ClientID:                clientID,
			ClientSecret:            clientSecret,
			Auditor:                 *auditprefix,
			RevocationManager:       revocationManager,
			BlessingDuration:        time.Duration(*minExpiryDays) * 24 * time.Hour,
			MacaroonBlessingService: naming.JoinAddressName(published[0], macaroonService),
		})
		if err != nil {
			vlog.Fatalf("Failed to create googleoauth handler: %v", err)
		}
		http.Handle(n, h)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		args := struct {
			Self                            security.PublicID
			RandomWeb                       bool
			GoogleServers, DischargeServers []string
			ListBlessingsRoute              string
		}{
			Self:      rt.R().Identity().PublicID(),
			RandomWeb: enableRandomHandler(),
		}
		if revocationManager != nil {
			args.DischargeServers = appendSuffixTo(published, dischargerService)
		}
		if len(*googleConfigChrome) > 0 || len(*googleConfigAndroid) > 0 {
			args.GoogleServers = appendSuffixTo(published, googleService)
		}
		if len(*auditprefix) > 0 && len(*googleConfigWeb) > 0 {
			args.ListBlessingsRoute = googleoauth.ListBlessingsRoute
		}
		if err := tmpl.Execute(w, args); err != nil {
			vlog.Info("Failed to render template:", err)
		}
	})
	vlog.Infof("Running HTTP server at: %v", httpaddress())
	go runHTTPServer(*httpaddr)
	<-signals.ShutdownOnSignals()
}

func appendSuffixTo(objectname []string, suffix string) []string {
	names := make([]string, len(objectname))
	for i, o := range objectname {
		names[i] = naming.JoinAddressName(o, suffix)
	}
	return names
}

// newDispatcher returns a dispatcher for both the blessing and the discharging service.
// their suffix. ReflectInvoker is used to invoke methods.
func newDispatcher(googleParams blesser.GoogleParams, macaroonKey []byte) ipc.Dispatcher {
	d := &dispatcher{
		invokers: map[string]ipc.Invoker{
			macaroonService:   ipc.ReflectInvoker(blesser.NewMacaroonBlesserServer(googleParams.R, macaroonKey)),
			dischargerService: ipc.ReflectInvoker(services.NewServerDischarger(discharger.NewDischarger(googleParams.R.Identity()))),
		},
		auth: vsecurity.NewACLAuthorizer(security.ACL{In: map[security.BlessingPattern]security.LabelSet{
			security.AllPrincipals: security.AllLabels,
		}}),
	}
	if len(*googleConfigChrome) > 0 || len(*googleConfigAndroid) > 0 {
		d.invokers[googleService] = ipc.ReflectInvoker(blesser.NewGoogleOAuthBlesserServer(googleParams))
	}
	return d
}

type dispatcher struct {
	invokers map[string]ipc.Invoker
	auth     security.Authorizer
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

func (d dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	if invoker := d.invokers[suffix]; invoker != nil {
		return invoker, d.auth, nil
	}
	return nil, nil, verror.NoExistf("%q is not a valid suffix at this server", suffix)
}

// Starts the blessing services and the discharging service on the same port.
func setupServices(r veyron2.Runtime, revocationManager *revocation.RevocationManager, macaroonKey []byte) (ipc.Server, []string, error) {
	var enable bool
	googleParams := blesser.GoogleParams{
		R:                 r,
		BlessingDuration:  time.Duration(*minExpiryDays) * 24 * time.Hour,
		DomainRestriction: *googleDomain,
		RevocationManager: revocationManager,
	}
	if clientID, ok := getOAuthClientID(*googleConfigChrome); ok {
		enable = true
		googleParams.AccessTokenClients = append(googleParams.AccessTokenClients, struct{ ID string }{clientID})
	}
	if clientID, ok := getOAuthClientID(*googleConfigAndroid); ok {
		enable = true
		googleParams.AccessTokenClients = append(googleParams.AccessTokenClients, struct{ ID string }{clientID})
	}
	if !enable {
		return nil, nil, nil
	}
	server, err := r.NewServer()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new ipc.Server: %v", err)
	}
	ep, err := server.ListenX(static.ListenSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("server.Listen(%s) failed: %v", static.ListenSpec, err)
	}
	googleParams.DischargerLocation = naming.JoinAddressName(ep.String(), dischargerService)

	dispatcher := newDispatcher(googleParams, macaroonKey)
	objectname := fmt.Sprintf("identity/%s", r.Identity().PublicID().Names()[0])
	if err := server.Serve(objectname, dispatcher); err != nil {
		return nil, nil, fmt.Errorf("failed to start Veyron services: %v", err)
	}
	vlog.Infof("Google blessing and discharger services enabled at endpoint %v and name %q", ep, objectname)

	published, _ := server.Published()
	if len(published) == 0 {
		// No addresses published, publish the endpoint instead (which may not be usable everywhere, but oh-well).
		published = append(published, ep.String())
	}
	return server, published, nil
}

func enableTLS() bool { return len(*tlsconfig) > 0 }
func enableRandomHandler() bool {
	return len(*googleConfigWeb)+len(*googleConfigChrome)+len(*googleConfigAndroid) == 0
}
func getOAuthClientID(config string) (clientID string, ok bool) {
	fname := config
	if len(fname) == 0 {
		return "", false
	}
	f, err := os.Open(fname)
	if err != nil {
		vlog.Fatalf("Failed to open %q: %v", fname, err)
	}
	defer f.Close()
	clientID, err = googleoauth.ClientIDFromJSON(f)
	if err != nil {
		vlog.Fatalf("Failed to decode JSON in %q: %v", fname, err)
	}
	return clientID, true
}
func getOAuthClientIDAndSecret(config string) (clientID, clientSecret string, ok bool) {
	fname := config
	if len(fname) == 0 {
		return "", "", false
	}
	f, err := os.Open(fname)
	if err != nil {
		vlog.Fatalf("Failed to open %q: %v", fname, err)
	}
	defer f.Close()
	clientID, clientSecret, err = googleoauth.ClientIDAndSecretFromJSON(f)
	if err != nil {
		vlog.Fatalf("Failed to decode JSON in %q: %v", fname, err)
	}
	return clientID, clientSecret, true
}
func runHTTPServer(addr string) {
	if !enableTLS() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			vlog.Fatalf("http.ListenAndServe failed: %v", err)
		}
		return
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

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts an HTTP server that mints veyron identities in response to GET requests.

To generate TLS certificates so the HTTP server can use SSL:
go run $GOROOT/src/pkg/crypto/tls/generate_cert.go --host <IP address>

To generate an identity for this service itself, use:
go install veyron/tools/identity && ./bin/identity generate <name> ><filename>
and set the VEYRON_IDENTITY environment variable when running this application.

To enable use of Google APIs to use Google OAuth for authorization, set --google_config,
which must point to the contents of a JSON file obtained after registering your application
with the Google Developer Console at:
https://code.google.com/apis/console
More details on Google OAuth at:
https://developers.google.com/accounts/docs/OAuth2Login

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}

func defaultHost() string {
	host, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("Failed to get hostname: %v", err)
	}
	return host
}

// providerIdentity returns the identity of the identity provider (i.e., this program) itself.
func providerIdentity() veyron2.ROpt {
	// TODO(ashankar): This scheme of initializing a runtime just to share the "load identity" code is ridiculous.
	// Figure out a way to update the runtime's identity with a wrapper and avoid this spurios "New" call.
	r, err := rt.New()
	if err != nil {
		vlog.Fatal(err)
	}
	defer r.Cleanup()
	id := r.Identity()
	if len(*auditprefix) > 0 {
		auditor, err := auditor.NewFileAuditor(*auditprefix)
		if err != nil {
			vlog.Fatal(err)
		}
		id = audit.NewPrivateID(id, auditor)
	}
	return options.RuntimeID{id}
}

func httpaddress() string {
	_, port, err := net.SplitHostPort(*httpaddr)
	if err != nil {
		vlog.Fatalf("Failed to parse %q: %v", *httpaddr, err)
	}
	scheme := "http"
	if enableTLS() {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%v", scheme, *host, port)
}

func dumpAuditLog() {
	if len(*auditprefix) == 0 {
		vlog.Fatalf("Must set --audit")
	}
	ch, err := auditor.ReadAuditLog(*auditprefix, *auditfilter)
	if err != nil {
		vlog.Fatal(err)
	}
	idx := 0
	for entry := range ch {
		fmt.Printf("%6d) %v\n", idx, entry)
		idx++
	}
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
<div class="page-header"><h2>{{.Self.Names}}</h2><h4>A Veyron Blessing Provider</h4></div>
<div class="well">
This is a Veyron identity provider that provides blessings with the name prefix <mark>{{.Self}}</mark>.
<br/>
The public key of this provider is {{.Self.PublicKey}}, which is available in <a class="btn btn-xs btn-primary" href="/pubkey/">DER</a> encoded
<a href="http://en.wikipedia.org/wiki/X.690#DER_encoding">format</a>.
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
email address as the name.</li>
{{end}}
{{if .RandomWeb}}
<li>You can obtain a randomly assigned PrivateID <a class="btn btn-sm btn-primary" href="/random/">here</a></li>
{{end}}
</ul>
</div>

</div>
</body>
</html>`))
