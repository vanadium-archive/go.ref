package rt_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron2"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	_ "v.io/core/veyron/profiles"
)

func init() {
	modules.RegisterChild("withRuntime", "", withRuntime)
	modules.RegisterChild("withoutRuntime", "", withoutRuntime)
}

func simpleEchoProgram(stdin io.Reader, stdout io.Writer) {
	fmt.Fprintf(stdout, "ready\n")
	scanner := bufio.NewScanner(stdin)
	if scanner.Scan() {
		fmt.Fprintf(stdout, "%s\n", scanner.Text())
	}
	modules.WaitForEOF(stdin)
}

func withRuntime(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	_, shutdown := veyron2.Init()
	defer shutdown()

	simpleEchoProgram(stdin, stdout)
	return nil
}

func withoutRuntime(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	simpleEchoProgram(stdin, stdout)
	return nil
}

func TestWithRuntime(t *testing.T) {
	sh, err := modules.NewShell(nil, nil)
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
	sh, err := modules.NewShell(nil, nil)
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
