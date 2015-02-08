package testdata

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"v.io/core/veyron/lib/testutil/v23tests"
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
	env := v23tests.New(t)
	defer env.Cleanup()

	neighborhood := fmt.Sprintf("test-%s-%d", getHostname(t), os.Getpid())
	v23tests.RunRootMT(env, "--veyron.tcp.address=127.0.0.1:0", "--neighborhood_name="+neighborhood)

	name, _ := env.GetVar("NAMESPACE_ROOT")

	clientBin := env.BuildGoPkg("v.io/core/veyron/tools/mounttable")

	// Get the neighborhood endpoint from the mounttable.
	neighborhoodEndpoint := clientBin.Start("glob", name, "nh").ExpectSetEventuallyRE(`^nh (.*) \(TTL .*\)$`)[0][1]

	if clientBin.Start("mount", name+"/myself", name, "5m").Wait(os.Stdout, os.Stderr) != nil {
		t.Fatalf("failed to mount the mounttable on itself")
	}
	if clientBin.Start("mount", name+"/google", "/www.google.com:80", "5m").Wait(os.Stdout, os.Stderr) != nil {
		t.Fatalf("failed to mount www.google.com")
	}

	// Test glob output. We expect three entries (two we mounted plus the
	// neighborhood). The 'myself' entry should be the IP:port we
	// specified for the mounttable.
	glob := clientBin.Start("glob", name, "*")
	matches := glob.ExpectSetEventuallyRE(
		`^google /www\.google\.com:80 \(TTL .*\)$`,
		`^myself (.*) \(TTL .*\)$`,
		`^nh `+regexp.QuoteMeta(neighborhoodEndpoint)+` \(TTL .*\)$`)
	if matches[1][1] != name {
		t.Fatalf("expected 'myself' entry to be %q, but was %q", name, matches[1][1])
	}

	// Test globbing on the neighborhood name. Its endpoint should be the
	// endpoint of the mount table.
	glob = clientBin.Start("glob", "/"+neighborhoodEndpoint, neighborhood)
	matches = glob.ExpectSetEventuallyRE("^" + regexp.QuoteMeta(neighborhood) + ` (.*) \(TTL .*\)$`)
	if matches[0][1] != name {
		t.Fatalf("expected endpoint of mount table for name %s", neighborhood)
	}
}
