// HTTP server that uses OAuth to create security.Blessings objects.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"v.io/core/veyron2/vlog"

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

	// If no tlsconfig has been provided, write and use our own.
	if flag.Lookup("tlsconfig").Value.String() == "" {
		writeCertAndKey()
		if err := flag.Set("tlsconfig", "./cert.pem,./key.pem"); err != nil {
			vlog.Fatal(err)
		}
	}

	server.NewIdentityServer(
		oauth.NewMockOAuth(),
		auditor,
		reader,
		revocationManager,
		oauthBlesserGoogleParams(revocationManager),
		caveats.NewMockCaveatSelector()).Serve()
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s starts a test version of the identityd server that
mocks out oauth, auditing, and revocation.

To generate TLS certificates so the HTTP server can use SSL:
go run $GOROOT/src/crypto/tls/generate_cert.go --host <IP address>

Flags:
`, os.Args[0])
	flag.PrintDefaults()
}

func writeCertAndKey() {
	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		vlog.Fatal("GOROOT not set")
	}
	generateCertFile := goroot + "/src/crypto/tls/generate_cert.go"
	host := flag.Lookup("host").Value.String()
	duration := 1 * time.Hour
	if err := exec.Command("go", "run", generateCertFile, "--host", host, "--duration", duration.String()).Run(); err != nil {
		vlog.Fatalf("Could not generate key and cert: %v", err)
	}
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
