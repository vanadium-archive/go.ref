// HTTP server that uses OAuth to create security.PrivateID objects.
package main

import (
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"veyron/lib/signals"
	"veyron/services/identity/blesser"
	"veyron/services/identity/googleoauth"
	"veyron/services/identity/handlers"
	"veyron/services/identity/util"
	"veyron2"
	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	httpaddr  = flag.String("httpaddr", "localhost:8125", "Address on which the HTTP server listens on.")
	tlsconfig = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. If empty, will not use HTTPS.")
	// TODO(ashankar): Revisit the choices for -vaddr and -vprotocol once the proxy design in relation to mounttables has been finalized.
	address       = flag.String("vaddr", "proxy.envyor.com:8100", "Address on which the Veyron blessing server listens on. Enabled iff --google_config is set")
	protocol      = flag.String("vprotocol", "veyron", "Protocol used to interpret --vaddr")
	host          = flag.String("host", defaultHost(), "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the public key of the signer at 'x.com/pubkey/'.")
	minExpiryDays = flag.Int("min_expiry_days", 365, "Minimum expiry time (in days) of identities issued by this server")

	googleConfigWeb       = flag.String("google_config_web", "", "Path to the JSON-encoded file containing the ClientID for web applications registered with the Google Developer Console. (Use the 'Download JSON' link on the Google APIs console).")
	googleConfigInstalled = flag.String("google_config_installed", "", "Path to the JSON-encoded file containing the ClientID for installed applications registered with the Google Developer console. (Use the 'Download JSON' link on the Google APIs console).")

	googleDomain = flag.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")
	generate     = flag.String("generate", "", "If non-empty, instead of running an HTTP server, a new identity will be created with the provided name and saved to --identity (if specified) and dumped to STDOUT in base64-encoded-vom")
	identity     = flag.String("identity", "", "Path to the file where the VOM-encoded security.PrivateID created with --generate will be written.")
)

func main() {
	// Setup flags and logging
	flag.Usage = usage
	r := rt.Init()
	defer r.Cleanup()

	if len(*generate) > 0 {
		generateAndSaveIdentity()
		return
	}

	// Setup handlers
	http.Handle("/pubkey/", handlers.Object{r.Identity().PublicID().PublicKey()}) // public key of this identity server
	if enableRandomHandler() {
		http.Handle("/random/", handlers.Random{r}) // mint identities with a random name
	}
	http.HandleFunc("/bless/", handlers.Bless) // use a provided PrivateID to bless a provided PublicID

	// Google OAuth
	var ipcServer ipc.Server
	if enabled, clientID, clientSecret := enableGoogleOAuth(*googleConfigInstalled); enabled {
		var err error
		if ipcServer, err = setupGoogleBlessingServer(r, clientID, clientSecret); err != nil {
			vlog.Fatalf("failed to setup veyron services for blessing: %v", err)
		}
		defer ipcServer.Stop()
	}
	if enabled, clientID, clientSecret := enableGoogleOAuth(*googleConfigWeb); enabled {
		_, port, err := net.SplitHostPort(*httpaddr)
		if err != nil {
			vlog.Fatalf("Failed to parse %q: %v", *httpaddr, err)
		}
		n := "/google/"
		http.Handle(n, googleoauth.NewHandler(googleoauth.HandlerArgs{
			UseTLS:              enableTLS(),
			Addr:                fmt.Sprintf("%s:%s", *host, port),
			Prefix:              n,
			ClientID:            clientID,
			ClientSecret:        clientSecret,
			MinExpiryDays:       *minExpiryDays,
			Runtime:             r,
			RestrictEmailDomain: *googleDomain,
		}))
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var servers []string
		if ipcServer != nil {
			servers, _ = ipcServer.Published()
		}
		args := struct {
			GoogleWeb, RandomWeb bool
			GoogleServers        []string
		}{
			GoogleWeb:     len(*googleConfigWeb) > 0,
			RandomWeb:     enableRandomHandler(),
			GoogleServers: servers,
		}
		if err := tmpl.Execute(w, args); err != nil {
			vlog.Info("Failed to render template:", err)
		}
	})
	go runHTTPServer(*httpaddr)
	<-signals.ShutdownOnSignals()
}

func setupGoogleBlessingServer(r veyron2.Runtime, clientID, clientSecret string) (ipc.Server, error) {
	server, err := r.NewServer()
	if err != nil {
		return nil, fmt.Errorf("failed to create new ipc.Server: %v", err)
	}
	ep, err := server.Listen(*protocol, *address)
	if err != nil {
		return nil, fmt.Errorf("server.Listen(%q, %q) failed: %v", "tcp", *address, err)
	}
	allowEveryoneACL := security.ACL{security.AllPrincipals: security.AllLabels}
	objectname := fmt.Sprintf("identity/%s/google", r.Identity().PublicID().Names()[0])
	if err := server.Serve(objectname, ipc.SoloDispatcher(blesser.NewGoogleOAuthBlesserServer(r, clientID, clientSecret, time.Duration(*minExpiryDays)*24*time.Hour, *googleDomain), security.NewACLAuthorizer(allowEveryoneACL))); err != nil {
		return nil, fmt.Errorf("failed to start Veyron service: %v", err)
	}
	vlog.Infof("Google blessing service enabled at endpoint %v and name %q", ep, objectname)
	return server, nil
}

func enableTLS() bool           { return len(*tlsconfig) > 0 }
func enableRandomHandler() bool { return len(*googleConfigInstalled)+len(*googleConfigWeb) == 0 }
func enableGoogleOAuth(config string) (enabled bool, clientID, clientSecret string) {
	fname := config
	if len(fname) == 0 {
		return false, "", ""
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
	return true, clientID, clientSecret
}

func runHTTPServer(addr string) {
	if !enableTLS() {
		vlog.Infof("Starting HTTP server (without TLS) at http://%v", addr)
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

func generateAndSaveIdentity() {
	id, err := rt.R().NewIdentity(*generate)
	if err != nil {
		vlog.Fatalf("Runtime.NewIdentity(%q) failed: %v", *generate, err)
	}
	if len(*identity) > 0 {
		if err = saveIdentity(*identity, id); err != nil {
			vlog.Fatalf("SaveIdentity %v: %v", *identity, err)
		}
	}
	b64, err := util.Base64VomEncode(id)
	if err != nil {
		vlog.Fatalf("Base64VomEncode(%q) failed: %v", id, err)
	}
	fmt.Println(b64)
}

func saveIdentity(filePath string, id security.PrivateID) error {
	f, err := os.OpenFile(filePath, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := security.SaveIdentity(f, id); err != nil {
		return err
	}
	return nil
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
<div class="page-header"><h1>Veyron Identity Generation</h1></div>
<div class="well">
This HTTP server mints veyron identities. The public key of the identity of this server is available in
<a class="btn btn-xs btn-info" href="/pubkey/base64vom">base64-encoded-vom-encoded</a> format.
</div>
{{if .GoogleServers}}
<div class="well">
The Veyron service for blessing is published at:
<tt>
{{range .GoogleServers}}{{.}}{{end}}
</tt>
</div>
{{end}}

{{if .GoogleWeb}}
<a class="btn btn-lg btn-primary" href="/google/auth">Google</a>
{{end}}
{{if .RandomWeb}}
<a class="btn btn-lg btn-primary" href="/random/">Random</a>
{{end}}
<a class="btn btn-lg btn-primary" href="/bless/">Bless As</a>

</div>
</body>
</html>`))
