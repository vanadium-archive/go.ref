// HTTP server that uses OAuth to create security.Blessings objects.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"v.io/core/veyron/services/identity/auditor"
	"v.io/core/veyron/services/identity/blesser"
	"v.io/core/veyron/services/identity/caveats"
	"v.io/core/veyron/services/identity/oauth"
	"v.io/core/veyron/services/identity/revocation"
	"v.io/core/veyron/services/identity/server"
)

var (
	googleDomain = flag.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	auditor, reader := auditor.NewMockBlessingAuditor()
	revocationManager := revocation.NewMockRevocationManager()

	server.NewIdentityServer(
		oauth.NewMockOAuth(),
		auditor,
		reader,
		revocationManager,
		oauthBlesserGoogleParams(revocationManager),
		caveats.NewMockCaveatSelector()).Serve()
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts an test version of the identityd server that
mocks out oauth, auditing, and revocation.

To generate TLS certificates so the HTTP server can use SSL:
go run $GOROOT/src/pkg/crypto/tls/generate_cert.go --host <IP address>

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}

func oauthBlesserGoogleParams(revocationManager revocation.RevocationManager) blesser.GoogleParams {
	googleParams := blesser.GoogleParams{
		BlessingDuration:  365 * 24 * time.Hour,
		DomainRestriction: *googleDomain,
		RevocationManager: revocationManager,
	}
	// TODO(suharshs): Figure out the test for this.
	return googleParams
}
