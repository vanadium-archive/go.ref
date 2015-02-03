package core

import (
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"v.io/core/veyron2"

	"v.io/core/veyron/lib/modules"

	"v.io/core/veyron/services/identity/auditor"
	"v.io/core/veyron/services/identity/blesser"
	"v.io/core/veyron/services/identity/caveats"
	"v.io/core/veyron/services/identity/oauth"
	"v.io/core/veyron/services/identity/revocation"
	"v.io/core/veyron/services/identity/server"
	"v.io/core/veyron/services/identity/util"
)

var (
	googleDomain = flag.CommandLine.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")
	host         = flag.CommandLine.String("host", "localhost", "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the root name and public key of the signer at 'x.com/blessing-root'.")
	httpaddr     = flag.CommandLine.String("httpaddr", "localhost:0", "Address on which the HTTP server listens on.")
	tlsconfig    = flag.CommandLine.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. This must be provided.")
)

func init() {
	modules.RegisterChild(TestIdentitydCommand, usage(flag.CommandLine), startTestIdentityd)
}

func startTestIdentityd(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	// Duration to use for tls cert and blessing duration.
	duration := 365 * 24 * time.Hour

	ctx, shutdown := veyron2.Init()
	defer shutdown()

	// If no tlsconfig has been provided, generate new cert and key and use them.
	if flag.CommandLine.Lookup("tlsconfig").Value.String() == "" {
		certFile, keyFile, err := util.WriteCertAndKey(*host, duration)
		if err != nil {
			return fmt.Errorf("Could not write cert and key: %v", err)
		}
		if err := flag.CommandLine.Set("tlsconfig", certFile+","+keyFile); err != nil {
			return fmt.Errorf("Could not set tlsconfig: %v", err)
		}
	}

	// Pick a free port if httpaddr flag is not set.
	// We can't use :0 here, because the identity server calls
	// http.ListenAndServeTLS, which blocks, leaving us with no way to tell
	// what port the server is running on.  Hence, we must pass in an
	// actual port so we know where the server is running.
	if flag.CommandLine.Lookup("httpaddr").Value.String() == flag.CommandLine.Lookup("httpaddr").DefValue {
		if err := flag.CommandLine.Set("httpaddr", "localhost:"+freePort()); err != nil {
			return fmt.Errorf("Could not set httpaddr: %v", err)
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

	s := server.NewIdentityServer(
		oauthProvider,
		auditor,
		reader,
		revocationManager,
		params,
		caveats.NewMockCaveatSelector())

	l := veyron2.GetListenSpec(ctx)

	_, veyronEPs, externalHttpaddress := s.Listen(ctx, &l, *host, *httpaddr, *tlsconfig)

	fmt.Fprintf(stdout, "TEST_IDENTITYD_NAME=%s\n", veyronEPs[0])
	fmt.Fprintf(stdout, "TEST_IDENTITYD_HTTP_ADDR=%s\n", externalHttpaddress)

	modules.WaitForEOF(stdin)
	return nil
}

func freePort() string {
	l, _ := net.Listen("tcp", ":0")
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}
