package modules_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/modules"
)

func init() {
	modules.RegisterChild("envtest", PrintEnv)
	modules.RegisterChild("echo", Echo)
	modules.RegisterChild("errortest", ErrorMain)
}

func Echo(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for _, a := range args {
		fmt.Fprintf(stdout, "stdout: %s\n", a)
		fmt.Fprintf(stderr, "stderr: %s\n", a)
	}
	return nil
}

func Print(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
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
		var stdout, stderr bytes.Buffer
		sh.Cleanup(&stdout, &stderr)
		if len(stdout.String()) != 0 {
			t.Errorf("unexpected stdout: %q", stdout.String())
		}
		if len(stderr.String()) != 0 {
			t.Errorf("unexpected stderr: %q", stderr.String())
		}
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
	if err := h.Shutdown(nil, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestChild(t *testing.T) {
	sh := modules.NewShell()
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	sh.AddSubprocess("envtest", "envtest: <variables to print>...")
	testCommand(t, sh, "envtest", key, val)
}

func TestFunction(t *testing.T) {
	sh := modules.NewShell()
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar & baz"
	sh.SetVar(key, val)
	sh.AddFunction("envtest", PrintEnv, "envtest: <variables to print>...")
	testCommand(t, sh, "envtest", key, val)
}

func TestErrorChild(t *testing.T) {
	sh := modules.NewShell()
	defer sh.Cleanup(nil, nil)
	sh.AddSubprocess("errortest", "")
	h, err := sh.Start("errortest")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil, nil), "exit status 1"; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func testShutdown(t *testing.T, sh *modules.Shell) {
	result := ""
	args := []string{"a", "b c", "ddd"}
	if _, err := sh.Start("echo", args...); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	sh.Cleanup(&stdoutBuf, &stderrBuf)
	stdoutOutput, stderrOutput := "stdout: echo\n", "stderr: echo\n"
	for _, a := range args {
		stdoutOutput += fmt.Sprintf("stdout: %s\n", a)
		stderrOutput += fmt.Sprintf("stderr: %s\n", a)
	}
	if got, want := stdoutBuf.String(), stdoutOutput+result; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got, want := stderrBuf.String(), stderrOutput; got != want {
		t.Errorf("got %q want %q", got, want)
	}

}

func TestShutdownSubprocess(t *testing.T) {
	sh := modules.NewShell()
	sh.AddSubprocess("echo", "[args]*")
	testShutdown(t, sh)
}

func TestShutdownFunction(t *testing.T) {
	sh := modules.NewShell()
	sh.AddFunction("echo", Echo, "[args]*")
	testShutdown(t, sh)
}

func TestErrorFunc(t *testing.T) {
	sh := modules.NewShell()
	defer sh.Cleanup(nil, nil)
	sh.AddFunction("errortest", ErrorMain, "")
	h, err := sh.Start("errortest")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil, nil), "an error"; got != nil && got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

// TODO(cnicolaou): more complete tests for environment variables,
// OS being overridden by Shell for example.
