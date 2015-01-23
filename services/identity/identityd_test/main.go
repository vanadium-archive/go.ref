// HTTP server that uses OAuth to create security.Blessings objects.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/vlog"

	_ "v.io/core/veyron/profiles/static"
	"v.io/core/veyron/services/identity/auditor"
	"v.io/core/veyron/services/identity/blesser"
	"v.io/core/veyron/services/identity/caveats"
	"v.io/core/veyron/services/identity/oauth"
	"v.io/core/veyron/services/identity/revocation"
	"v.io/core/veyron/services/identity/server"
	"v.io/core/veyron/services/identity/util"
)

var (
	googleDomain = flag.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")

	// Flags controlling the HTTP server
	host      = flag.String("host", "localhost", "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the root name and public key of the signer at 'x.com/blessing-root'.")
	httpaddr  = flag.String("httpaddr", "localhost:8125", "Address on which the HTTP server listens on.")
	tlsconfig = flag.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files, in that order. This must be provided.")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	// Duration to use for tls cert and blessing duration.
	duration := 365 * 24 * time.Hour

	// If no tlsconfig has been provided, write and use our own.
	if flag.Lookup("tlsconfig").Value.String() == "" {
		certFile, keyFile, err := util.WriteCertAndKey(*host, duration)
		if err != nil {
			vlog.Fatal(err)
		}
		if err := flag.Set("tlsconfig", certFile+","+keyFile); err != nil {
			vlog.Fatal(err)
		}
	}

	auditor, reader := auditor.NewMockBlessingAuditor()
	revocationManager := revocation.NewMockRevocationManager()
	oauthProvider := oauth.NewMockOAuth()

	params := blesser.OAuthBlesserParams{
		OAuthProvider:     oauthProvider,
		BlessingDuration:  duration,
		DomainRestriction: *googleDomain,
		RevocationManager: revocationManager,
	}

	ctx, shutdown := veyron2.Init()
	defer shutdown()

	listenSpec := veyron2.GetListenSpec(ctx)
	s := server.NewIdentityServer(
		oauthProvider,
		auditor,
		reader,
		revocationManager,
		params,
		caveats.NewMockCaveatSelector())
	s.Serve(ctx, &listenSpec, *host, *httpaddr, *tlsconfig)
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts a test version of the identityd server that
mocks out oauth, auditing, and revocation.

To generate TLS certificates so the HTTP server can use SSL:
go run $(go list -f {{.Dir}} "crypto/tls")/generate_cert.go --host <IP address>

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}
