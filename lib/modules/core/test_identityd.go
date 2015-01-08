package core

import (
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"v.io/core/veyron2/rt"

	"v.io/core/veyron/lib/flags"
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
	ifs *flag.FlagSet = flag.NewFlagSet("test_identityd", flag.ContinueOnError)

	googleDomain = ifs.String("google_domain", "", "An optional domain name. When set, only email addresses from this domain are allowed to authenticate via Google OAuth")
	host         = ifs.String("host", "localhost", "Hostname the HTTP server listens on. This can be the name of the host running the webserver, but if running behind a NAT or load balancer, this should be the host name that clients will connect to. For example, if set to 'x.com', Veyron identities will have the IssuerName set to 'x.com' and clients can expect to find the root name and public key of the signer at 'x.com/blessing-root'.")
	httpaddr     = ifs.String("httpaddr", "localhost:0", "Address on which the HTTP server listens on.")
	tlsconfig    = ifs.String("tlsconfig", "", "Comma-separated list of TLS certificate and private key files. This must be provided.")

	ifl *flags.Flags = flags.CreateAndRegister(ifs, flags.Listen)
)

func init() {
	modules.RegisterChild(TestIdentitydCommand, usage(ifs), startTestIdentityd)
}

func startTestIdentityd(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := parseFlags(ifl, args); err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}

	// Duration to use for tls cert and blessing duration.
	duration := 365 * 24 * time.Hour

	// If no tlsconfig has been provided, generate new cert and key and use them.
	if ifs.Lookup("tlsconfig").Value.String() == "" {
		certFile, keyFile, err := util.WriteCertAndKey(*host, duration)
		if err != nil {
			return fmt.Errorf("Could not write cert and key: %v", err)
		}
		if err := ifs.Set("tlsconfig", certFile+","+keyFile); err != nil {
			return fmt.Errorf("Could not set tlsconfig: %v", err)
		}
	}

	// Pick a free port if httpaddr flag is not set.
	// We can't use :0 here, because the identity server calles
	// http.ListenAndServeTLS, which block, leaving us with no way to tell
	// what port the server is running on.  Hence, we must pass in an
	// actual port so we know where the server is running.
	if ifs.Lookup("httpaddr").Value.String() == ifs.Lookup("httpaddr").DefValue {
		if err := ifs.Set("httpaddr", "localhost:"+freePort()); err != nil {
			return fmt.Errorf("Could not set httpaddr: %v", err)
		}
	}

	auditor, reader := auditor.NewMockBlessingAuditor()
	revocationManager := revocation.NewMockRevocationManager()

	googleParams := blesser.GoogleParams{
		BlessingDuration:  duration,
		DomainRestriction: *googleDomain,
		RevocationManager: revocationManager,
	}

	s := server.NewIdentityServer(
		oauth.NewMockOAuth(),
		auditor,
		reader,
		revocationManager,
		googleParams,
		caveats.NewMockCaveatSelector())

	l := initListenSpec(ifl)
	r, err := rt.New()
	if err != nil {
		return fmt.Errorf("rt.New() failed: %v", err)
	}
	defer r.Cleanup()

	ipcServer, veyronEPs, externalHttpaddress := s.Listen(r, &l, *host, *httpaddr, *tlsconfig)
	defer ipcServer.Stop()

	fmt.Fprintf(stdout, "TEST_IDENTITYD_ADDR=%s\n", veyronEPs[0])
	fmt.Fprintf(stdout, "TEST_IDENTITYD_HTTP_ADDR=%s\n", externalHttpaddress)

	modules.WaitForEOF(stdin)
	return nil
}

func freePort() string {
	l, _ := net.Listen("tcp", ":0")
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}
