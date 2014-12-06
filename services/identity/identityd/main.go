// HTTP server that uses OAuth to create security.Blessings objects.
package main

import (
	"crypto/rand"
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"html/template"
	"io/ioutil"
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
	"veyron.io/veyron/veyron/security/audit"
	"veyron.io/veyron/veyron/services/identity/auditor"
	"veyron.io/veyron/veyron/services/identity/blesser"
	"veyron.io/veyron/veyron/services/identity/googleoauth"
	"veyron.io/veyron/veyron/services/identity/handlers"
	"veyron.io/veyron/veyron/services/identity/revocation"
	services "veyron.io/veyron/veyron/services/security"
	"veyron.io/veyron/veyron/services/security/discharger"

	"veyron.io/veyron/veyron/profiles/static"
)

var (
	// Flags controlling the HTTP server
	httpaddr  = flag.String("httpaddr", "localhost:8125", "Address on which the HTTP server listens on.")
	tlsconfig = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. This must be provided.")
	host      = flag.String("host", defaultHost(), "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the public key of the signer at 'x.com/pubkey/'.")

	// Flag controlling auditing and revocation of Blessing operations.
	sqlConfig = flag.String("sqlconfig", "", "Path to file containing go-sql-driver connection string of the following form: [username[:password]@][protocol[(address)]]/dbname")

	// Configuration for various Google OAuth-based clients.
	googleConfigWeb     = flag.String("google_config_web", "", "Path to JSON-encoded OAuth client configuration for the web application that renders the audit log for blessings provided by this provider.")
	googleConfigChrome  = flag.String("google_config_chrome", "", "Path to the JSON-encoded OAuth client configuration for Chrome browser applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleConfigAndroid = flag.String("google_config_android", "", "Path to the JSON-encoded OAuth client configuration for Android applications that obtain blessings from this server (via the OAuthBlesser.BlessUsingAccessToken RPC) from this server.")
	googleDomain        = flag.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")
)

const (
	googleService     = "google"
	macaroonService   = "macaroon"
	dischargerService = "discharger"
)

func main() {
	flag.Usage = usage
	flag.Parse()

	var sqlDB *sql.DB
	var err error
	if len(*sqlConfig) > 0 {
		config, err := ioutil.ReadFile(*sqlConfig)
		if err != nil {
			vlog.Fatalf("failed to read sql config from %v", *sqlConfig)
		}
		sqlDB, err = dbFromConfigDatabase(strings.Trim(string(config), "\n"))
		if err != nil {
			vlog.Fatalf("failed to create sqlDB: %v", err)
		}
	}

	p, blessingLogReader := providerPrincipal(sqlDB)
	runtime, err := rt.New(options.RuntimePrincipal{p})
	if err != nil {
		vlog.Fatal(err)
	}
	defer runtime.Cleanup()

	var revocationManager *revocation.RevocationManager
	// Only set revocationManager sqlConfig (and thus sqlDB) is set.
	if sqlDB != nil {
		revocationManager, err = revocation.NewRevocationManager(sqlDB)
		if err != nil {
			vlog.Fatalf("Failed to start RevocationManager: %v", err)
		}
	}

	// Setup handlers
	http.Handle("/pubkey/", handlers.PublicKey{runtime.Principal().PublicKey()}) // public key of this server
	macaroonKey := make([]byte, 32)
	if _, err := rand.Read(macaroonKey); err != nil {
		vlog.Fatalf("macaroonKey generation failed: %v", err)
	}
	// Google OAuth
	ipcServer, published, err := setupServices(runtime, revocationManager, macaroonKey)
	if err != nil {
		vlog.Fatalf("Failed to setup veyron services for blessing: %v", err)
	}
	defer ipcServer.Stop()
	if clientID, clientSecret, ok := getOAuthClientIDAndSecret(*googleConfigWeb); ok {
		n := "/google/"
		h, err := googleoauth.NewHandler(googleoauth.HandlerArgs{
			R:                       runtime,
			MacaroonKey:             macaroonKey,
			Addr:                    fmt.Sprintf("%s%s", httpaddress(), n),
			ClientID:                clientID,
			ClientSecret:            clientSecret,
			BlessingLogReader:       blessingLogReader,
			RevocationManager:       revocationManager,
			MacaroonBlessingService: naming.JoinAddressName(published[0], macaroonService),
		})
		if err != nil {
			vlog.Fatalf("Failed to create HTTP handler for google-based authentication: %v", err)
		}
		http.Handle(n, h)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		args := struct {
			Self                            security.Blessings
			RandomWeb                       bool
			GoogleServers, DischargeServers []string
			ListBlessingsRoute              string
		}{
			Self:      runtime.Principal().BlessingStore().Default(),
			RandomWeb: enableRandomHandler(),
		}
		if revocationManager != nil {
			args.DischargeServers = appendSuffixTo(published, dischargerService)
		}
		if len(*googleConfigChrome) > 0 || len(*googleConfigAndroid) > 0 {
			args.GoogleServers = appendSuffixTo(published, googleService)
		}
		if sqlDB != nil && len(*googleConfigWeb) > 0 {
			args.ListBlessingsRoute = googleoauth.ListBlessingsRoute
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

// newDispatcher returns a dispatcher for both the blessing and the
// discharging service.
func newDispatcher(googleParams blesser.GoogleParams, macaroonKey []byte) ipc.Dispatcher {
	d := dispatcher(map[string]interface{}{
		macaroonService:   blesser.NewMacaroonBlesserServer(macaroonKey),
		dischargerService: services.DischargerServer(discharger.NewDischarger()),
	})
	if len(*googleConfigChrome) > 0 || len(*googleConfigAndroid) > 0 {
		d[googleService] = blesser.NewGoogleOAuthBlesserServer(googleParams)
	}
	return d
}

type allowEveryoneAuthorizer struct{}

func (allowEveryoneAuthorizer) Authorize(security.Context) error { return nil }

type dispatcher map[string]interface{}

func (d dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if invoker := d[suffix]; invoker != nil {
		return invoker, allowEveryoneAuthorizer{}, nil
	}
	return nil, nil, verror.NoExistf("%q is not a valid suffix at this server", suffix)
}

// Starts the blessing services and the discharging service on the same port.
func setupServices(r veyron2.Runtime, revocationManager *revocation.RevocationManager, macaroonKey []byte) (ipc.Server, []string, error) {
	googleParams := blesser.GoogleParams{
		// TODO(ashankar,nlacasse): Figure out how to have web-appications use the "caveats" form and
		// always select an expiry instead of forcing a ridiculously large value here.
		BlessingDuration:  365 * 24 * time.Hour,
		DomainRestriction: *googleDomain,
		RevocationManager: revocationManager,
	}
	if clientID, ok := getOAuthClientID(*googleConfigChrome); ok {
		googleParams.AccessTokenClients = append(googleParams.AccessTokenClients, blesser.AccessTokenClient{Name: "chrome", ClientID: clientID})
	}
	if clientID, ok := getOAuthClientID(*googleConfigAndroid); ok {
		googleParams.AccessTokenClients = append(googleParams.AccessTokenClients, blesser.AccessTokenClient{Name: "android", ClientID: clientID})
	}
	server, err := r.NewServer()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create new ipc.Server: %v", err)
	}
	ep, err := server.Listen(static.ListenSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("server.Listen(%v) failed: %v", static.ListenSpec, err)
	}
	googleParams.DischargerLocation = naming.JoinAddressName(ep.String(), dischargerService)

	dispatcher := newDispatcher(googleParams, macaroonKey)
	objectname := naming.Join("identity", fmt.Sprintf("%v", r.Principal().BlessingStore().Default()))
	if err := server.ServeDispatcher(objectname, dispatcher); err != nil {
		return nil, nil, fmt.Errorf("failed to start Veyron services: %v", err)
	}
	vlog.Infof("Google blessing and discharger services enabled at %v", naming.JoinAddressName(ep.String(), objectname))
	published, _ := server.Published()
	if len(published) == 0 {
		// No addresses published, publish the endpoint instead (which may not be usable everywhere, but oh-well).
		published = append(published, ep.String())
	}
	return server, published, nil
}

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

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts an HTTP server that brokers blessings after authenticating through OAuth.

To generate TLS certificates so the HTTP server can use SSL:
go run $GOROOT/src/pkg/crypto/tls/generate_cert.go --host <IP address>

To use Google as an OAuth provider the --google_config_* flags must be set to point to
the a JSON file obtained after registering the application with the Google Developer Console
at https://cloud.google.com/console

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

// providerPrincipal returns the Principal to use for the identity provider (i.e., this program) and
// the database where audits will be store. If no database exists nil will be returned.
func providerPrincipal(sqlDB *sql.DB) (security.Principal, auditor.BlessingLogReader) {
	// TODO(ashankar): Somewhat silly to have to create a runtime, but oh-well.
	r, err := rt.New()
	if err != nil {
		vlog.Fatal(err)
	}
	defer r.Cleanup()
	p := r.Principal()
	if sqlDB == nil {
		return p, nil
	}
	auditor, reader, err := auditor.NewSQLBlessingAuditor(sqlDB)
	if err != nil {
		vlog.Fatalf("Failed to create sql auditor from config: %v", err)
	}
	return audit.NewPrincipal(p, auditor), reader
}

func dbFromConfigDatabase(database string) (*sql.DB, error) {
	db, err := sql.Open("mysql", database+"?parseTime=true")
	if err != nil {
		return nil, fmt.Errorf("failed to create database with database(%v): %v", database, err)
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
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
email address.</li>
{{end}}
</ul>
</div>

</div>
</body>
</html>`))
