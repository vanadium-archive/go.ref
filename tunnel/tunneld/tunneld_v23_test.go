package main_test

//go:generate v23 test generate .

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/lib/testutil/v23tests"
)

func V23TestTunneld(t *v23tests.T) {
	v23tests.RunRootMT(t, "--veyron.tcp.address=127.0.0.1:0")

	tunneldBin := t.BuildGoPkg("v.io/apps/tunnel/tunneld")
	vsh := t.BuildGoPkg("v.io/apps/tunnel/vsh")
	mounttableBin := t.BuildGoPkg("v.io/core/veyron/tools/mounttable")

	port, err := testutil.FindUnusedPort()
	if err != nil {
		t.Fatalf("FindUnusedPort failed: %v", err)
	}

	tunnelAddress := fmt.Sprintf("127.0.0.1:%d", port)
	tunnelEndpoint := "/" + tunnelAddress

	// Start tunneld with a known endpoint.
	tunneldBin.Start("--veyron.tcp.address=" + tunnelAddress)

	// Run remote command with the endpoint.
	if want, got := "HELLO ENDPOINT\n", vsh.Start(tunnelEndpoint, "echo", "HELLO", "ENDPOINT").Output(); want != got {
		t.Fatalf("unexpected output, got %s, want %s", got, want)
	}

	// Run remote command with the object name.
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname() failed: %v", err)
	}

	if want, got := "HELLO NAME\n", vsh.Start("tunnel/hostname/"+hostname, "echo", "HELLO", "NAME").Output(); want != got {
		t.Fatalf("unexpected output, got %s, want %s", got, want)
	}

	// Send input to remote command.
	want := "HELLO SERVER"
	if got := vsh.WithStdin(bytes.NewBufferString(want)).Start(tunnelEndpoint, "cat").Output(); want != got {
		t.Fatalf("unexpected output, got %s, want %s", got, want)
	}

	// And again with a file redirection this time.
	outDir := t.NewTempDir()
	outPath := filepath.Join(outDir, "hello.txt")

	// TODO(sjr): instead of using Output() here, we'd really rather do
	// WaitOrDie(os.Stdout, os.Stderr). There is currently a race caused by
	// WithStdin that makes this flaky.
	vsh.WithStdin(bytes.NewBufferString(want)).Start(tunnelEndpoint, "cat > "+outPath).Output()
	if got, err := ioutil.ReadFile(outPath); err != nil || string(got) != want {
		if err != nil {
			t.Fatalf("ReadFile(%v) failed: %v", outPath, err)
		} else {
			t.Fatalf("unexpected output, got %s, want %s", got, want)
		}
	}

	// Verify that all published names are there.
	root, _ := t.GetVar("NAMESPACE_ROOT")
	inv := mounttableBin.Start("glob", root, "tunnel/*/*")

	// Expect two entries: one for the tunnel hostname and one for its hwaddr.
	matches := inv.ExpectSetEventuallyRE(
		"tunnel/hostname/"+regexp.QuoteMeta(hostname)+" (.*) \\(TTL .*\\)",
		"tunnel/hwaddr/.* (.*) \\(TTL .*\\)")

	// The full endpoint should contain the address we initially specified for the tunnel.
	if want = "@" + tunnelAddress + "@"; !strings.Contains(matches[0][1], want) {
		t.Fatalf("expected tunnel endpoint %s to contain %s, but it did not", matches[0][1], want)
	}

	// The hwaddr endpoint should be the same as the hostname endpoint.
	if matches[0][1] != matches[1][1] {
		t.Fatalf("expected hwaddr and hostname tunnel endpoints to match, but they did not (%s != %s)", matches[0][1], matches[1][1])
	}
}
