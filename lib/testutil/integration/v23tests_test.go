package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/lib/testutil/integration"
	_ "v.io/core/veyron/profiles"
)

// TODO(sjr): add more unit tests, especially for errors cases.
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
	proxyName := proxy.ExpectVar("NAME")
	proxyAddress, _ := naming.SplitAddressName(proxyName)

	re := regexp.MustCompile("proxyd (.*) \\(.*")
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		inv := nsBin.Start("glob", "...")
		parts := re.FindStringSubmatch(inv.ReadLine())
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

// The next set of tests are a complicated dance to test the correct
// detection and logging of failed integration tests. The complication
// is that we need to run these tests in a child process so that we
// can examine their output, but not in the parent process. We use the
// modules framework to do so, with the added twist that we need access
// to an instance of testing.T which we obtain via a global variable.
// TODO(cnicolaou): this will need to change once we switch to using
// TestMain.
func IntegrationTestInChild(i integration.T) {
	fmt.Println("Hello")
	sleep := i.BinaryFromPath("/bin/sleep")
	sleep.Start("3600")
	s2 := sleep.Start("6400")
	sleep.Start("21600")
	s2.Kill(syscall.SIGTERM)
	s2.Wait(nil, nil)
	i.FailNow()
	panic("should never get here")
}

var globalT *testing.T

func TestHelperProcess(t *testing.T) {
	globalT = t
	modules.DispatchInTest()
}

func RunIntegrationTestInChild(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	integration.RunTest(globalT, IntegrationTestInChild)
	return nil
}

func init() {
	testutil.Init()
	modules.RegisterChild("RunIntegrationTestInChild", "", RunIntegrationTestInChild)
}

func TestDeferHandling(t *testing.T) {
	sh, _ := modules.NewShell(nil, nil)
	child, err := sh.Start("RunIntegrationTestInChild", nil, "--v23.tests")
	if err != nil {
		t.Fatal(err)
	}
	s := expect.NewSession(t, child.Stdout(), time.Minute)
	s.Expect("Hello")
	s.ExpectRE("--- FAIL: TestHelperProcess", -1)
	for _, e := range []string{
		".* 0: /bin/sleep: shutdown status: has not been shutdown",
		".* 1: /bin/sleep: shutdown status: signal: terminated",
		".* 2: /bin/sleep: shutdown status: has not been shutdown",
	} {
		s.ExpectRE(e, -1)
	}
	var stderr bytes.Buffer
	if err := child.Shutdown(nil, &stderr); err != nil {
		if !strings.Contains(err.Error(), "exit status 1") {
			t.Fatal(err)
		}
	}
	vlog.Infof("Child\n=============\n%s", stderr.String())
	vlog.Infof("-----------------")
}
