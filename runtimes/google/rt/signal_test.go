package rt_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/appcycle"
	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
)

func init() {
	modules.RegisterChild("withRuntime", "", withRuntime)
	modules.RegisterChild("withoutRuntime", "", withoutRuntime)
}

// A fack profile to explicitly request the Google runtime.
type myprofile struct{}

func (mp *myprofile) Name() string {
	return "test"
}

func (mp *myprofile) Runtime() (string, []veyron2.ROpt) {
	return "google", nil
}

func (mp *myprofile) Platform() *veyron2.Platform {
	return &veyron2.Platform{"google", nil, "v1", "any", "rel1", ".2", "who knows", "this host"}
}

func (mp *myprofile) String() string {
	return "myprofile on " + mp.Platform().String()
}

func (mp *myprofile) Init(rt veyron2.Runtime, _ *config.Publisher) (veyron2.AppCycle, error) {
	return appcycle.New(rt), nil
}

func (mp *myprofile) Cleanup() {}

func simpleEchoProgram(stdin io.Reader, stdout io.Writer) {
	fmt.Fprintf(stdout, "ready\n")
	scanner := bufio.NewScanner(stdin)
	if scanner.Scan() {
		fmt.Fprintf(stdout, "%s\n", scanner.Text())
	}
	modules.WaitForEOF(stdin)
}

func withRuntime(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	// Make sure that we use "google" runtime implementation in this
	// package even though we have to use the public API which supports
	// arbitrary runtime implementations.
	r, err := rt.New(options.Profile{&myprofile{}})
	if err != nil {
		return err
	}
	defer r.Cleanup()
	simpleEchoProgram(stdin, stdout)
	return nil
}

func withoutRuntime(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	simpleEchoProgram(stdin, stdout)
	return nil
}

func TestWithRuntime(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start("withRuntime", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer h.Shutdown(os.Stderr, os.Stderr)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("ready")
	syscall.Kill(h.Pid(), syscall.SIGHUP)
	h.Stdin().Write([]byte("foo\n"))
	s.Expect("foo")
	h.CloseStdin()
	s.ExpectEOF()
}

func TestWithoutRuntime(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start("withoutRuntime", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer h.Shutdown(os.Stderr, os.Stderr)
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("ready")
	syscall.Kill(h.Pid(), syscall.SIGHUP)
	s.ExpectEOF()
	err = h.Shutdown(os.Stderr, os.Stderr)
	want := "exit status 2"
	if err == nil || err.Error() != want {
		t.Errorf("got %s, want %s", err, want)

	}
}
