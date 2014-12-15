package modules_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"

	"veyron.io/veyron/veyron2/security"
)

const credentialsEnvPrefix = "\"" + consts.VeyronCredentials + "="

func init() {
	testutil.Init()
	modules.RegisterChild("envtest", "envtest: <variables to print>...", PrintFromEnv)
	modules.RegisterChild("printenv", "printenv", PrintEnv)
	modules.RegisterChild("echos", "[args]*", Echo)
	modules.RegisterChild("errortestChild", "", ErrorMain)
	modules.RegisterChild("ignores_stdin", "", ignoresStdin)

	modules.RegisterFunction("envtestf", "envtest: <variables to print>...", PrintFromEnv)
	modules.RegisterFunction("echof", "[args]*", Echo)
	modules.RegisterFunction("errortestFunc", "", ErrorMain)
}

func ignoresStdin(io.Reader, io.Writer, io.Writer, map[string]string, ...string) error {
	<-time.After(time.Minute)
	return nil
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
	h, err := sh.Start(name, nil, key)
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
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtest", key, val)
}

func TestChildNoRegistration(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtest", key, val)
	_, err = sh.Start("non-existent-command", nil, "random", "args")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestFunction(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar & baz"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtestf", key, val)
}

func TestErrorChild(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start("errortestChild", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil, nil), "exit status 255"; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func testShutdown(t *testing.T, sh *modules.Shell, command string, isfunc bool) {
	result := ""
	args := []string{"a", "b c", "ddd"}
	if _, err := sh.Start(command, nil, args...); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	sh.Cleanup(&stdoutBuf, &stderrBuf)
	stdoutOutput, stderrOutput := "stdout: "+command+"\n", "stderr: "+command+"\n"
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
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	testShutdown(t, sh, "echos", false)
}

// TestShutdownSubprocessIgnoresStdin verifies that Shutdown doesn't wait
// forever if a child does not die upon closing stdin; but instead times out and
// returns an appropriate error.
func TestShutdownSubprocessIgnoresStdin(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	sh.SetWaitTimeout(time.Second)
	h, err := sh.Start("ignores_stdin", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	if err := sh.Cleanup(&stdoutBuf, &stderrBuf); err == nil || err.Error() != exec.ErrTimeout.Error() {
		t.Errorf("unexpected error in Cleanup: got %v, want %v", err, exec.ErrTimeout)
	}
	if err := syscall.Kill(h.Pid(), syscall.SIGINT); err != nil {
		t.Errorf("Kill failed: %v", err)
	}
}

// TestStdoutRace exemplifies a potential race between reading from child's
// stdout and closing stdout in Wait (called by Shutdown).
//
// NOTE: triggering the actual --race failure is hard, even if the
// implementation inappropriately sets stdout to the file that is to be closed
// in Wait.
func TestStdoutRace(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	sh.SetWaitTimeout(time.Second)
	h, err := sh.Start("ignores_stdin", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	ch := make(chan error, 1)
	go func() {
		buf := make([]byte, 5)
		// This will block since the child is not writing anything on
		// stdout.
		_, err := h.Stdout().Read(buf)
		ch <- err
	}()
	// Give the goroutine above a chance to run, so that we're blocked on
	// stdout.Read.
	<-time.After(time.Second)
	// Cleanup should close stdout, and unblock the goroutine.
	sh.Cleanup(nil, nil)
	if got, want := <-ch, io.EOF; got != want {
		t.Errorf("Expected %v, got %v instead", want, got)
	}

	if err := syscall.Kill(h.Pid(), syscall.SIGINT); err != nil {
		t.Errorf("Kill failed: %v", err)
	}
}

func TestShutdownFunction(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	testShutdown(t, sh, "echof", true)
}

func TestErrorFunc(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start("errortestFunc", nil)
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
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	sh.SetVar("a", "1")
	sh.SetVar("b", "2")
	args := []string{"oh", "ah"}
	h, err := sh.Start("printenv", nil, args...)
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

	foundVeyronCredentials := false
	for _, want := range shEnv {
		if strings.HasPrefix(want, credentialsEnvPrefix) {
			foundVeyronCredentials = true
			continue
		}
		if !find(want, childEnv) {
			t.Errorf("failed to find %s in %#v", want, childEnv)
		}
	}
	if !foundVeyronCredentials {
		t.Errorf("%v environment variable not set in command envelope", consts.VeyronCredentials)
	}

	foundVeyronCredentials = false
	for _, want := range childEnv {
		if want == "\""+exec.VersionVariable+"=\"" {
			continue
		}
		if strings.HasPrefix(want, credentialsEnvPrefix) {
			foundVeyronCredentials = true
			continue
		}
		if !find(want, shEnv) {
			t.Errorf("failed to find %s in %#v", want, shEnv)
		}
	}
	if !foundVeyronCredentials {
		t.Errorf("%v environment variable not set for command", consts.VeyronCredentials)
	}
}

func TestEnvCredentials(t *testing.T) {
	startChildAndGetCredentials := func(sh *modules.Shell, env []string) string {
		h, err := sh.Start("printenv", env)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		defer h.Shutdown(os.Stderr, os.Stderr)
		scanner := bufio.NewScanner(h.Stdout())
		for scanner.Scan() {
			o := scanner.Text()
			if strings.HasPrefix(o, credentialsEnvPrefix) {
				return strings.TrimSuffix(strings.TrimPrefix(o, credentialsEnvPrefix), "\"")
			}
		}
		return ""
	}
	validateCredentials := func(dir string, wantBlessing string) error {
		if len(dir) == 0 {
			return errors.New("credentials directory not found")
		}
		p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
		if err != nil {
			return err
		}
		def := p.BlessingStore().Default()
		if blessingsSharedWithAll := p.BlessingStore().ForPeer("random_peer"); !reflect.DeepEqual(blessingsSharedWithAll, def) {
			return fmt.Errorf("Blessing shared with all peers: %v is different from default Blessings: %v", blessingsSharedWithAll, def)
		}
		if got := def.ForContext(security.NewContext(&security.ContextParams{LocalPrincipal: p})); len(got) != 1 || got[0] != wantBlessing {
			return fmt.Errorf("got blessings: %v, want exactly one blessing: %v", got, wantBlessing)
		}
		return nil
	}

	// Test child credentials when runtime is not initialized and VeyronCredentials
	// is not set.
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := validateCredentials(startChildAndGetCredentials(sh, nil), "test-shell/child"); err != nil {
		t.Fatal(err)
	}

	// Test that no two children have the same credentials directory.
	if cred1, cred2 := startChildAndGetCredentials(sh, nil), startChildAndGetCredentials(sh, nil); cred1 == cred2 {
		t.Fatalf("The same credentials directory %v was set for two children", cred1)
	}
	sh.Cleanup(nil, nil)

	// Test child credentials when VeyronCredentials are set.
	old := os.Getenv(consts.VeyronCredentials)
	defer os.Setenv(consts.VeyronCredentials, old)
	dir, _ := tsecurity.NewCredentials("os")
	defer os.RemoveAll(dir)
	if err := os.Setenv(consts.VeyronCredentials, dir); err != nil {
		t.Fatal(err)
	}
	sh, err = modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := validateCredentials(startChildAndGetCredentials(sh, nil), "os/child"); err != nil {
		t.Fatal(err)
	}
	sh.Cleanup(nil, nil)

	// Test that os VeyronCredentials are not deleted by the Cleanup.
	if finfo, err := os.Stat(dir); err != nil || !finfo.IsDir() {
		t.Fatalf("%q is not a directory", dir)
	}

	// Test that VeyronCredentials specified on the shell override the OS ones.
	dir, _ = tsecurity.NewCredentials("shell")
	defer os.RemoveAll(dir)
	sh, err = modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	sh.SetVar(consts.VeyronCredentials, dir)
	if err := validateCredentials(startChildAndGetCredentials(sh, nil), "shell/child"); err != nil {
		t.Fatal(err)
	}
	sh.ClearVar(consts.VeyronCredentials)
	if credentials := startChildAndGetCredentials(sh, nil); len(credentials) != 0 {
		t.Fatalf("found credentials %v set for child even when shell credentials were cleared", credentials)
	}
	anotherDir, _ := tsecurity.NewCredentials("anotherShell")
	defer os.RemoveAll(anotherDir)
	sh.SetVar(consts.VeyronCredentials, anotherDir)
	if err := validateCredentials(startChildAndGetCredentials(sh, nil), "anotherShell/child"); err != nil {
		t.Fatal(err)
	}
	sh.Cleanup(nil, nil)

	// Test that VeyronCredentials specified as a parameter overrides the OS and
	// shell ones.
	dir, _ = tsecurity.NewCredentials("param")
	defer os.RemoveAll(dir)
	env := []string{consts.VeyronCredentials + "=" + dir}
	if err := validateCredentials(startChildAndGetCredentials(sh, env), "param"); err != nil {
		t.Fatal(err)
	}

}

func TestEnvMerge(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	sh.SetVar("a", "1")
	os.Setenv("a", "wrong, should be 1")
	sh.SetVar("b", "2 also wrong")
	os.Setenv("b", "wrong, should be 2")
	h, err := sh.Start("printenv", []string{"b=2"})
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

func TestSetEntryPoint(t *testing.T) {
	env := map[string]string{"a": "a", "b": "b"}
	nenv := modules.SetEntryPoint(env, "eg1")
	if got, want := len(nenv), 3; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	nenv = modules.SetEntryPoint(env, "eg2")
	if got, want := len(nenv), 3; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	sort.Strings(nenv)
	if got, want := nenv, []string{"VEYRON_SHELL_HELPER_PROCESS_ENTRY_POINT=eg2", "a=a", "b=b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

// TODO(cnicolaou): test for error return from cleanup
