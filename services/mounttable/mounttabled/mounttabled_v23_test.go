package main_test

import (
	"fmt"
	"os"
	"regexp"

	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

func getHostname(i *v23tests.T) string {
	if hostname, err := os.Hostname(); err != nil {
		i.Fatalf("Hostname() failed: %v", err)
		return ""
	} else {
		return hostname
	}
}

func binaryWithCredentials(i *v23tests.T, extension, pkgpath string) *v23tests.Binary {
	creds, err := i.Shell().NewChildCredentials(extension)
	if err != nil {
		i.Fatalf("NewCustomCredentials (for %q) failed: %v", pkgpath, err)
	}
	b := i.BuildV23Pkg(pkgpath)
	return b.WithStartOpts(b.StartOpts().WithCustomCredentials(creds))
}

func V23TestMount(i *v23tests.T) {
	neighborhood := fmt.Sprintf("test-%s-%d", getHostname(i), os.Getpid())
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0", "--neighborhood_name="+neighborhood)

	name, _ := i.GetVar("NAMESPACE_ROOT")
	clientBin := binaryWithCredentials(i, "cmd", "v.io/x/ref/cmd/mounttable")

	// Get the neighborhood endpoint from the mounttable.
	neighborhoodEndpoint := clientBin.Start("glob", name, "nh").ExpectSetEventuallyRE(`^nh (.*) \(TTL .*\)$`)[0][1]

	if err := clientBin.Start("mount", "--blessing_pattern=...", name+"/myself", name, "5m").Wait(os.Stdout, os.Stderr); err != nil {
		i.Fatalf("failed to mount the mounttable on itself: %v", err)
	}
	if err := clientBin.Start("mount", "--blessing_pattern=...", name+"/google", "/www.google.com:80", "5m").Wait(os.Stdout, os.Stderr); err != nil {
		i.Fatalf("failed to mount www.google.com: %v", err)
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
		i.Fatalf("expected 'myself' entry to be %q, but was %q", name, matches[1][1])
	}

	// Test globbing on the neighborhood name. Its endpoint should be the
	// endpoint of the mount table.
	glob = clientBin.Start("glob", "/"+neighborhoodEndpoint, neighborhood)
	matches = glob.ExpectSetEventuallyRE("^" + regexp.QuoteMeta(neighborhood) + ` (.*) \(TTL .*\)$`)
	if matches[0][1] != name {
		i.Fatalf("expected endpoint of mount table for name %s", neighborhood)
	}
}
