// HTTP server that uses OAuth to create security.PrivateID objects.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"veyron/services/identity/googleoauth"
	"veyron/services/identity/handlers"
	"veyron/services/identity/util"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vlog"
)

var (
	port          = flag.Int("port", 8125, "Port number on which the HTTP server listens on.")
	host          = flag.String("host", defaultHost(), "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the public key of the signer at 'x.com/pubkey/'.")
	tlsconfig     = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. If empty, will not use HTTPS.")
	minExpiryDays = flag.Int("min_expiry_days", 365, "Minimum expiry time (in days) of identities issued by this server")
	googleConfig  = flag.String("google_config", "", "Path to the JSON-encoded file containing the ClientID for web applications registered with the Google Developer Console. (Use the 'Download JSON' link on the Google APIs console).")

	generate = flag.String("generate", "", "If non-empty, instead of running an HTTP server, a new identity will be created with the provided name and saved to --identity (if specified) and dumped to STDOUT in base64-encoded-vom")
	identity = flag.String("identity", "", "Path to the file where the VOM-encoded security.PrivateID created with --generate will be written.")
)

func main() {
	// Setup flags and logging
	flag.Usage = usage
	r := rt.Init()
	defer r.Shutdown()

	if len(*generate) > 0 {
		generateAndSaveIdentity()
		return
	}

	// Setup handlers
	http.HandleFunc("/", handleMain)
	http.Handle("/pubkey/", handlers.Object{r.Identity().PublicID().PublicKey()}) // public key of this identity server
	if enableRandomHandler() {
		http.Handle("/random/", handlers.Random{r}) // mint identities with a random name
	}
	http.HandleFunc("/bless/", handlers.Bless) // use a provided PrivateID to bless a provided PublicID
	// Google OAuth
	if enableGoogleOAuth() {
		f, err := os.Open(*googleConfig)
		if err != nil {
			vlog.Fatalf("Failed to open %q: %v", *googleConfig, err)
		}
		clientid, secret, err := googleoauth.ClientIDAndSecretFromJSON(f)
		if err != nil {
			vlog.Fatalf("Failed to decode %q: %v", *googleConfig, err)
		}
		f.Close()
		n := "/google/"
		http.Handle(n, googleoauth.NewHandler(googleoauth.HandlerArgs{
			UseTLS:        enableTLS(),
			Addr:          fmt.Sprintf("%s:%d", *host, *port),
			Prefix:        n,
			ClientID:      clientid,
			ClientSecret:  secret,
			MinExpiryDays: *minExpiryDays,
			Runtime:       r,
		}))
	}
	startHTTPServer(*port)
}

func enableTLS() bool           { return len(*tlsconfig) > 0 }
func enableGoogleOAuth() bool   { return len(*googleConfig) > 0 }
func enableRandomHandler() bool { return !enableGoogleOAuth() }

func startHTTPServer(port int) {
	addr := fmt.Sprintf(":%d", port)
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

func handleMain(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`
<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Identity Server</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.0.0/css/bootstrap.min.css">
</head>
<body>
<div class="container">
<div class="page-header"><h1>Veyron Identity Generation</h1></div>
<div class="well">
This HTTP server mints veyron identities. The public key of the identity of this server is available in
<a class="btn btn-xs btn-info" href="/pubkey/base64vom">base64-encoded-vom-encoded</a> format.
</div>`))
	if enableGoogleOAuth() {
		w.Write([]byte(`<a class="btn btn-lg btn-primary" href="/google/auth">Google</a> `))
	}
	if enableRandomHandler() {
		w.Write([]byte(`<a class="btn btn-lg btn-primary" href="/random/">Random</a> `))
	}
	w.Write([]byte(`<a class="btn btn-lg btn-primary" href="/bless/">Bless As</a>
</div>
</body>
</html>`))
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
