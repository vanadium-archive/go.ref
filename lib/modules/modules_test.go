package modules_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"veyron/lib/modules"
)

func init() {
	modules.RegisterChild("envtest", PrintEnv)
	modules.RegisterChild("errortest", ErrorMain)
}

func PrintEnv(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for _, a := range args {
		if v := env[a]; len(v) > 0 {
			fmt.Fprintf(stdout, a+"="+v+"\n")
		} else {
			fmt.Fprintf(stderr, "missing %s\n", a)
		}
	}
	modules.WaitForEOF(stdin)
	fmt.Fprintf(stdout, "done\n")
	return nil
}

func ErrorMain(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return fmt.Errorf("an error")
}

func waitForInput(scanner *bufio.Scanner) bool {
	ch := make(chan struct{})
	go func(ch chan<- struct{}) {
		scanner.Scan()
		ch <- struct{}{}
	}(ch)
	select {
	case <-ch:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

func testCommand(t *testing.T, sh *modules.Shell, name, key, val string) {
	h, err := sh.Start(name, key)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer func() {
		sh.Cleanup(os.Stderr)
	}()
	scanner := bufio.NewScanner(h.Stdout())
	if !waitForInput(scanner) {
		t.Errorf("timeout")
		return
	}
	if got, want := scanner.Text(), key+"="+val; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	h.CloseStdin()
	if !waitForInput(scanner) {
		t.Errorf("timeout")
		return
	}
	if got, want := scanner.Text(), "done"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := h.Shutdown(nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestChild(t *testing.T) {
	sh := modules.NewShell()
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	sh.AddSubprocess("envtest", "envtest: <variables to print>...")
	testCommand(t, sh, "envtest", key, val)
}

func TestFunction(t *testing.T) {
	sh := modules.NewShell()
	key, val := "simpleVar", "foo & bar & baz"
	sh.SetVar(key, val)
	sh.AddFunction("envtest", PrintEnv, "envtest: <variables to print>...")
	testCommand(t, sh, "envtest", key, val)
}

func TestErrorChild(t *testing.T) {
	sh := modules.NewShell()
	sh.AddSubprocess("errortest", "")
	h, err := sh.Start("errortest")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil), "exit status 1"; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestErrorFunc(t *testing.T) {
	sh := modules.NewShell()
	sh.AddFunction("errortest", ErrorMain, "")
	h, err := sh.Start("errortest")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil), "an error"; got != nil && got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHelperProcess(t *testing.T) {
	if !modules.IsTestHelperProcess() {
		return
	}
	if err := modules.Dispatch(); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

// TODO(cnicolaou): more complete tests for environment variables,
// OS being overridden by Shell for example.
