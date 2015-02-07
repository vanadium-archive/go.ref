package testdata

import (
	"bytes"
	"regexp"
	"testing"
	"time"

	"v.io/core/veyron2/naming"

	"v.io/core/veyron/lib/testutil/integration"
	_ "v.io/core/veyron/profiles"
)

// TODO(sjr): add more unit tests, especially for errors.
// TODO(sjr): need to make sure processes don't get left lying around.

func TestBinaryFromPath(t *testing.T) {
	env := integration.New(t)
	defer env.Cleanup()

	bash := env.BinaryFromPath("/bin/bash")
	if want, got := "hello world\n", bash.Start("-c", "echo hello world").Output(); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}

	inv := bash.Start("-c", "echo hello world")
	var buf bytes.Buffer
	inv.WaitOrDie(&buf, nil)
	if want, got := "hello world\n", buf.String(); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}
}

func TestMountTable(t *testing.T) {
	env := integration.New(t)
	defer env.Cleanup()

	integration.RunRootMT(env, "--veyron.tcp.address=127.0.0.1:0")
	proxyBin := env.BuildGoPkg("v.io/core/veyron/services/proxy/proxyd")
	nsBin := env.BuildGoPkg("v.io/core/veyron/tools/namespace")

	mt, ok := env.GetVar("NAMESPACE_ROOT")
	if !ok || len(mt) == 0 {
		t.Fatalf("expected a mount table name")
	}
	proxy := proxyBin.Start("--address=127.0.0.1:0", "-name=proxyd")
	proxyName := proxy.Session().ExpectVar("NAME")
	proxyAddress, _ := naming.SplitAddressName(proxyName)

	re := regexp.MustCompile("proxyd (.*) \\(.*")
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		inv := nsBin.Start("glob", "...")
		parts := re.FindStringSubmatch(inv.Session().ReadLine())
		if len(parts) == 2 {
			if got, want := proxyAddress, parts[1]; got != want {
				t.Fatalf("got: %v, want: %v", got, want)
			} else {
				break
			}
		}
	}
	if got, want := proxy.Exists(), true; got != want {
		t.Fatalf("got: %v, want: %v", got, want)
	}
}
