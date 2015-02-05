package testdata

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/lib/testutil/integration"
	_ "v.io/core/veyron/profiles/static"
)

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func pickUnusedPort(t *testing.T) int {
	if port, err := testutil.FindUnusedPort(); err != nil {
		t.Fatalf("FindUnusedPort failed: %v", err)
		return 0
	} else {
		return port
	}
}

func getHostname(t *testing.T) string {
	if hostname, err := os.Hostname(); err != nil {
		t.Fatalf("Hostname() failed: %v", err)
		return ""
	} else {
		return hostname
	}
}

func TestMount(t *testing.T) {
	env := integration.New(t)
	defer env.Cleanup()

	// Build and run mounttabled. The test environment already gives us
	// one, but we want to run it with our own options (e.g.
	// --neighborhood_name), and currently the test environment does not
	// let us customize the execution of the automatically-provided
	// mounttabled.
	serverBin := env.BuildGoPkg("v.io/core/veyron/services/mounttable/mounttabled")
	serverPort := pickUnusedPort(t)
	address := fmt.Sprintf("127.0.0.1:%d", serverPort)
	endpoint := fmt.Sprintf("/%s", address)
	neighborhood := fmt.Sprintf("test-%s-%d", getHostname(t), os.Getpid())
	inv := serverBin.Start(fmt.Sprintf("--veyron.tcp.address=127.0.0.1:%d", serverPort), "--neighborhood_name="+neighborhood)
	defer inv.Kill(syscall.SIGTERM)

	clientBin := env.BuildGoPkg("v.io/core/veyron/tools/mounttable")

	// Get the neighborhood endpoint from the mounttable.
	e := expect.NewSession(t, clientBin.Start("glob", endpoint, "nh").Stdout(), 10*time.Second)
	neighborhoodEndpoint := e.ExpectSetEventuallyRE(`^nh (.*) \(TTL .*\)$`)[0][1]

	if clientBin.Start("mount", endpoint+"/myself", endpoint, "5m").Wait(os.Stdout, os.Stderr) != nil {
		t.Fatalf("failed to mount the mounttable on itself")
	}
	if clientBin.Start("mount", endpoint+"/google", "/www.google.com:80", "5m").Wait(os.Stdout, os.Stderr) != nil {
		t.Fatalf("failed to mount www.google.com")
	}

	// Test glob output. We expect three entries (two we mounted plus the
	// neighborhood). The 'myself' entry should be the IP:port we
	// specified for the mounttable.
	e = expect.NewSession(t, clientBin.Start("glob", endpoint, "*").Stdout(), 10*time.Second)
	matches := e.ExpectSetEventuallyRE(
		`^google /www\.google\.com:80 \(TTL .*\)$`,
		`^myself (.*) \(TTL .*\)$`,
		`^nh `+regexp.QuoteMeta(neighborhoodEndpoint)+` \(TTL .*\)$`)
	if matches[1][1] != endpoint {
		t.Fatalf("expected 'myself' entry to be %s, but was %s", endpoint, matches[1][1])
	}

	// Test globbing on the neighborhood name. Its endpoint should contain
	// the IP:port we specified for the mounttable.
	e = expect.NewSession(t, clientBin.Start("glob", "/"+neighborhoodEndpoint, neighborhood).Stdout(), 10*time.Second)
	matches = e.ExpectSetEventuallyRE("^" + regexp.QuoteMeta(neighborhood) + ` (.*) \(TTL .*\)$`)
	if want := "@" + address + "@"; !strings.Contains(matches[0][1], want) {
		t.Fatalf("expected neighborhood endpoint to contain %s, but did not. Was: %s", want, matches[0][1])
	}

}
