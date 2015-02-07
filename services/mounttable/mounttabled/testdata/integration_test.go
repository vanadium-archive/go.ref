package testdata

import (
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/testutil/integration"
	_ "v.io/core/veyron/profiles/static"
)

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

	neighborhood := fmt.Sprintf("test-%s-%d", getHostname(t), os.Getpid())
	integration.RunRootMT(env, "--veyron.tcp.address=127.0.0.1:0", "--neighborhood_name="+neighborhood)

	name, _ := env.GetVar("NAMESPACE_ROOT")

	clientBin := env.BuildGoPkg("v.io/core/veyron/tools/mounttable")

	// Get the neighborhood endpoint from the mounttable.
	neighborhoodEndpoint := clientBin.Start("glob", name, "nh").Session().ExpectSetEventuallyRE(`^nh (.*) \(TTL .*\)$`)[0][1]

	if clientBin.Start("mount", name+"/myself", name, "5m").Wait(os.Stdout, os.Stderr) != nil {
		t.Fatalf("failed to mount the mounttable on itself")
	}
	if clientBin.Start("mount", name+"/google", "/www.google.com:80", "5m").Wait(os.Stdout, os.Stderr) != nil {
		t.Fatalf("failed to mount www.google.com")
	}

	// Test glob output. We expect three entries (two we mounted plus the
	// neighborhood). The 'myself' entry should be the IP:port we
	// specified for the mounttable.
	e := expect.NewSession(t, clientBin.Start("glob", name, "*").Stdout(), 10*time.Second)
	matches := e.ExpectSetEventuallyRE(
		`^google /www\.google\.com:80 \(TTL .*\)$`,
		`^myself (.*) \(TTL .*\)$`,
		`^nh `+regexp.QuoteMeta(neighborhoodEndpoint)+` \(TTL .*\)$`)
	if matches[1][1] == name {
		t.Fatalf("expected 'myself' entry to be %s, but was %s", name, matches[1][1])
	}

	// Test globbing on the neighborhood name. Its endpoint should be the
	// endpoint of the mount table.
	e = expect.NewSession(t, clientBin.Start("glob", "/"+neighborhoodEndpoint, neighborhood).Stdout(), 10*time.Second)
	matches = e.ExpectSetEventuallyRE("^" + regexp.QuoteMeta(neighborhood) + ` (.*) \(TTL .*\)$`)
	if matches[0][1] != name {
		t.Fatalf("expected endpoint of mount table for name %s", neighborhood)
	}
}
