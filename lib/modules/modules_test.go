package modules_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil"
)

func init() {
	testutil.Init()
	modules.RegisterChild("envtest", "envtest: <variables to print>...", PrintFromEnv)
	modules.RegisterChild("printenv", "printenv", PrintEnv)
	modules.RegisterChild("echo", "[args]*", Echo)
	modules.RegisterChild("errortest", "", ErrorMain)
}

func Echo(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for _, a := range args {
		fmt.Fprintf(stdout, "stdout: %s\n", a)
		fmt.Fprintf(stderr, "stderr: %s\n", a)
	}
	return nil
}

func PrintFromEnv(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for _, a := range args[1:] {
		if v := env[a]; len(v) > 0 {
			fmt.Fprintf(stdout, "%s\n", a+"="+v)
		} else {
			fmt.Fprintf(stderr, "missing %s\n", a)
		}
	}
	modules.WaitForEOF(stdin)
	fmt.Fprintf(stdout, "done\n")
	return nil
}

const printEnvArgPrefix = "PRINTENV_ARG="

func PrintEnv(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for _, a := range args {
		fmt.Fprintf(stdout, "%s%s\n", printEnvArgPrefix, a)
	}
	for k, v := range env {
		fmt.Fprintf(stdout, "%q\n", k+"="+v)
	}
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
		t.Fatalf("timeout")
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
	sh := modules.NewShell("envtest")
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtest", key, val)
}

func TestChildNoRegistration(t *testing.T) {
	sh := modules.NewShell()
	defer sh.Cleanup(os.Stderr, os.Stderr)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtest", key, val)
	_, err := sh.Start("non-existent-command", "random", "args")
	if err == nil || err.Error() != `Shell command "non-existent-command" not registered` {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestFunction(t *testing.T) {
	sh := modules.NewShell(".*")
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar & baz"
	sh.SetVar(key, val)
	sh.AddFunction("envtestf", PrintFromEnv, "envtest: <variables to print>...")
	testCommand(t, sh, "envtestf", key, val)
}

func TestErrorChild(t *testing.T) {
	sh := modules.NewShell("errortest")
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start("errortest")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil, nil), "exit status 255"; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func testShutdown(t *testing.T, sh *modules.Shell, isfunc bool) {
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
	if !isfunc {
		stderrBuf.ReadString('\n') // Skip past the random # generator output
	}
	if got, want := stderrBuf.String(), stderrOutput; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestShutdownSubprocess(t *testing.T) {
	sh := modules.NewShell("echo")
	testShutdown(t, sh, false)
}

func TestShutdownFunction(t *testing.T) {
	sh := modules.NewShell()
	sh.AddFunction("echo", Echo, "[args]*")
	testShutdown(t, sh, true)
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

func find(want string, in []string) bool {
	for _, a := range in {
		if a == want {
			return true
		}
	}
	return false
}

func TestEnvelope(t *testing.T) {
	sh := modules.NewShell("printenv")
	defer sh.Cleanup(nil, nil)
	sh.SetVar("a", "1")
	sh.SetVar("b", "2")
	args := []string{"oh", "ah"}
	h, err := sh.Start("printenv", args...)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	scanner := bufio.NewScanner(h.Stdout())
	childArgs, childEnv := []string{}, []string{}
	for scanner.Scan() {
		o := scanner.Text()
		if strings.HasPrefix(o, printEnvArgPrefix) {
			childArgs = append(childArgs, strings.TrimPrefix(o, printEnvArgPrefix))
		} else {
			childEnv = append(childEnv, o)
		}
	}
	shArgs, shEnv := sh.CommandEnvelope("printenv", nil, args...)
	for i, ev := range shEnv {
		shEnv[i] = fmt.Sprintf("%q", ev)
	}
	for _, want := range args {
		if !find(want, childArgs) {
			t.Errorf("failed to find %q in %s", want, childArgs)
		}
		if !find(want, shArgs) {
			t.Errorf("failed to find %q in %s", want, shArgs)
		}
	}

	for _, want := range shEnv {
		if !find(want, childEnv) {
			t.Errorf("failed to find %s in %#v", want, childEnv)
		}
	}
	for _, want := range childEnv {
		if want == "\""+exec.VersionVariable+"=\"" {
			continue
		}
		if !find(want, shEnv) {
			t.Errorf("failed to find %s in %#v", want, shEnv)
		}
	}
}

func TestEnvMerge(t *testing.T) {
	sh := modules.NewShell("printenv")
	defer sh.Cleanup(nil, nil)
	sh.SetVar("a", "1")
	os.Setenv("a", "wrong, should be 1")
	sh.SetVar("b", "2 also wrong")
	os.Setenv("b", "wrong, should be 2")

	h, err := sh.StartWithEnv("printenv", []string{"b=2"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	scanner := bufio.NewScanner(h.Stdout())
	for scanner.Scan() {
		o := scanner.Text()
		if strings.HasPrefix(o, "a=") {
			if got, want := o, "a=1"; got != want {
				t.Errorf("got: %q, want %q", got, want)
			}
		}
		if strings.HasPrefix(o, "b=") {
			if got, want := o, "b=2"; got != want {
				t.Errorf("got: %q, want %q", got, want)
			}
		}
	}
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

// TODO(cnicolaou): more complete tests for environment variables,
// OS being overridden by Shell for example.
//
// TODO(cnicolaou): test for one or either of the io.Writers being nil
// on calls to Shutdown
//
// TODO(cnicolaou): test for error return from cleanup
