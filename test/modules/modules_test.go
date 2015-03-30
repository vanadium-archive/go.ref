// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/verror"

	"v.io/x/ref/lib/exec"
	execconsts "v.io/x/ref/lib/exec/consts"
	"v.io/x/ref/lib/flags/consts"
	_ "v.io/x/ref/profiles"
	vsecurity "v.io/x/ref/security"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

const credentialsEnvPrefix = "\"" + consts.VeyronCredentials + "="

func init() {
	modules.RegisterChild("envtest", "envtest: <variables to print>...", PrintFromEnv)
	modules.RegisterChild("printenv", "printenv", PrintEnv)
	modules.RegisterChild("printblessing", "printblessing", PrintBlessing)
	modules.RegisterChild("echos", "[args]*", Echo)
	modules.RegisterChild("errortestChild", "", ErrorMain)
	modules.RegisterChild("ignores_stdin", "", ignoresStdin)
	modules.RegisterChild("pipeProc", "", pipeEcho)
	modules.RegisterChild("lifo", "", lifo)

	modules.RegisterFunction("envtestf", "envtest: <variables to print>...", PrintFromEnv)
	modules.RegisterFunction("echof", "[args]*", Echo)
	modules.RegisterFunction("errortestFunc", "", ErrorMain)
	modules.RegisterFunction("pipeFunc", "", pipeEcho)
}

// We must call Testmain ourselves because using v23 test generate
// creates an import cycle for this package.
func TestMain(m *testing.M) {
	test.Init()
	if modules.IsModulesChildProcess() {
		if err := modules.Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
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

func pipeEcho(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		fmt.Fprintf(stdout, "%p: %s\n", pipeEcho, scanner.Text())
	}
	return nil
}

func lifo(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	scanner := bufio.NewScanner(stdin)
	scanner.Scan()
	msg := scanner.Text()
	modules.WaitForEOF(stdin)
	fmt.Fprintf(stdout, "%p: %s\n", lifo, msg)
	return nil
}

func PrintBlessing(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	blessing := v23.GetPrincipal(ctx).BlessingStore().Default()
	fmt.Fprintf(stdout, "%s", blessing)
	return nil
}

func PrintFromEnv(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for _, a := range args {
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
		want := ""
		if testing.Verbose() {
			want = "---- Shell Cleanup ----\n"
		}
		if got := stdout.String(); got != "" && got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if got := stderr.String(); got != "" && got != want {
			t.Errorf("got %q, want %q", got, want)
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

func getBlessing(t *testing.T, sh *modules.Shell, env ...string) string {
	h, err := sh.Start("printblessing", env)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	scanner := bufio.NewScanner(h.Stdout())
	if !waitForInput(scanner) {
		t.Errorf("timeout")
		return ""
	}
	return scanner.Text()
}

func getCustomBlessing(t *testing.T, sh *modules.Shell, creds *modules.CustomCredentials) string {
	h, err := sh.StartWithOpts(sh.DefaultStartOpts().WithCustomCredentials(creds), nil, "printblessing")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	scanner := bufio.NewScanner(h.Stdout())
	if !waitForInput(scanner) {
		t.Errorf("timeout")
		return ""
	}
	return scanner.Text()
}

func TestChild(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtest", key, val)
}

func TestAgent(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stdout, os.Stderr)
	a := getBlessing(t, sh)
	b := getBlessing(t, sh)
	if a != b {
		t.Errorf("Expected same blessing for children, got %s and %s", a, b)
	}
}

func TestCustomPrincipal(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	p, err := vsecurity.NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.BlessSelf("myshell")
	if err != nil {
		t.Fatal(err)
	}
	if err := vsecurity.SetDefaultBlessings(p, b); err != nil {
		t.Fatal(err)
	}
	cleanDebug := p.BlessingStore().DebugString()
	sh, err := modules.NewShell(ctx, p, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stdout, os.Stderr)
	if got, want := getBlessing(t, sh), "myshell/child"; got != want {
		t.Errorf("Bad blessing. Got %q, want %q", got, want)
	}
	newDebug := p.BlessingStore().DebugString()
	if cleanDebug != newDebug {
		t.Errorf("Shell modified custom principal. Was:\n%q\nNow:\n%q", cleanDebug, newDebug)
	}
}

func TestCustomCredentials(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	root := testutil.NewIDProvider("myshell")
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Cleanup(os.Stdout, os.Stderr)

	newCreds := func(ext string) *modules.CustomCredentials {
		p, err := sh.NewCustomCredentials()
		if err != nil {
			t.Fatal(err)
		}
		b, err := root.NewBlessings(p.Principal(), ext)
		if err != nil {
			t.Fatal(err)
		}
		if err := vsecurity.SetDefaultBlessings(p.Principal(), b); err != nil {
			t.Fatal(err)
		}
		return p
	}

	a := newCreds("a")
	cleanDebug := a.Principal().BlessingStore().DebugString()

	blessing := getCustomBlessing(t, sh, a)
	if blessing != "myshell/a" {
		t.Errorf("Bad blessing. Expected myshell/a, go %q", blessing)
	}

	b := newCreds("bar")
	blessing = getCustomBlessing(t, sh, b)
	if blessing != "myshell/bar" {
		t.Errorf("Bad blessing. Expected myshell/bar, go %q", blessing)
	}

	// Make sure we can re-use credentials
	blessing = getCustomBlessing(t, sh, a)
	if blessing != "myshell/a" {
		t.Errorf("Bad blessing. Expected myshell/a, go %q", blessing)
	}

	newDebug := a.Principal().BlessingStore().DebugString()
	if cleanDebug != newDebug {
		t.Errorf("Shell modified custom principal. Was:\n%q\nNow:\n%q", cleanDebug, newDebug)
	}
}

func createCredentials(blessing string) (string, error) {
	dir, err := ioutil.TempDir("", "TestNoAgent_v23_credentials")
	if err != nil {
		return "", err
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, nil)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	b, err := p.BlessSelf(blessing)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	if err := vsecurity.SetDefaultBlessings(p, b); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func TestNoAgent(t *testing.T) {
	const noagent = "noagent"
	creds, err := createCredentials(noagent)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(creds)
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stdout, os.Stderr)
	if got, want := getBlessing(t, sh, fmt.Sprintf("VEYRON_CREDENTIALS=%s", creds)), noagent; got != want {
		t.Errorf("Bad blessing. Got %q, want %q", got, want)
	}
}

func TestChildNoRegistration(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtest", key, val)
	_, err = sh.Start("non-existent-command", nil, "random", "args")
	if err == nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		t.Fatalf("expected error")
	}
}

func TestFunction(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar & baz"
	sh.SetVar(key, val)
	testCommand(t, sh, "envtestf", key, val)
}

func TestErrorChild(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start("errortestChild", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil, nil), "exit status 1"; got == nil || got.Error() != want {
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
	var stdoutOutput, stderrOutput string
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
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, false, t)
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
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, false, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts.ShutdownTimeout = time.Second
	h, err := sh.StartWithOpts(opts, nil, "ignores_stdin")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	if err := sh.Cleanup(&stdoutBuf, &stderrBuf); err == nil || verror.ErrorID(err) != exec.ErrTimeout.ID {
		t.Errorf("unexpected error in Cleanup: got %v, want %v", err, exec.ErrTimeout.ID)
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
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts.ShutdownTimeout = time.Second
	h, err := sh.StartWithOpts(opts, nil, "ignores_stdin")
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
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, false, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	testShutdown(t, sh, "echof", true)
}

func TestErrorFunc(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
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
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
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

	for _, want := range shEnv {
		if !find(want, childEnv) {
			t.Errorf("failed to find %s in %#v", want, childEnv)
		}
	}

	for _, want := range childEnv {
		if want == "\""+execconsts.ExecVersionVariable+"=\"" {
			continue
		}
		if !find(want, shEnv) {
			t.Errorf("failed to find %s in %#v", want, shEnv)
		}
	}
}

func TestEnvMerge(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
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

func TestNoExec(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.StartWithOpts(sh.DefaultStartOpts().NoExecCommand(), nil, "/bin/echo", "hello", "world")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	scanner := bufio.NewScanner(h.Stdout())
	scanner.Scan()
	if got, want := scanner.Text(), "hello world"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExternal(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	cookie := strconv.Itoa(rand.Int())
	sh.SetConfigKey("cookie", cookie)
	h, err := sh.StartWithOpts(sh.DefaultStartOpts().ExternalCommand(), nil, os.Args[0], "--test.run=TestExternalTestHelper")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	scanner := bufio.NewScanner(h.Stdout())
	scanner.Scan()
	if got, want := scanner.Text(), fmt.Sprintf("cookie: %s", cookie); got != want {
		h.Shutdown(os.Stderr, os.Stderr)
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestExternalTestHelper is used by TestExternal above and has not utility
// as a test in it's own right.
func TestExternalTestHelper(t *testing.T) {
	child, err := exec.GetChildHandle()
	if err != nil {
		return
	}
	child.SetReady()
	val, err := child.Config.Get("cookie")
	if err != nil {
		t.Fatalf("failed to get child handle: %s", err)
	}
	fmt.Printf("cookie: %s\n", val)
}

func TestPipe(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)

	for _, cmd := range []string{"pipeProc", "pipeFunc"} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		opts := sh.DefaultStartOpts()
		opts.Stdin = r
		h, err := sh.StartWithOpts(opts, nil, cmd)
		if err != nil {
			t.Fatal(err)
		}
		cookie := strconv.Itoa(rand.Int())
		go func(w *os.File, s string) {
			fmt.Fprintf(w, "hello world\n")
			fmt.Fprintf(w, "%s\n", s)
			w.Close()
		}(w, cookie)

		scanner := bufio.NewScanner(h.Stdout())
		want := []string{
			fmt.Sprintf("%p: hello world", pipeEcho),
			fmt.Sprintf("%p: %s", pipeEcho, cookie),
		}
		i := 0
		for scanner.Scan() {
			if got, want := scanner.Text(), want[i]; got != want {
				t.Fatalf("%s: got %v, want %v", cmd, got, want)
			}
			i++
		}
		if got, want := i, 2; got != want {
			t.Fatalf("%s: got %v, want %v", cmd, got, want)
		}
		if err := h.Shutdown(os.Stderr, os.Stderr); err != nil {
			t.Fatal(err)
		}
		r.Close()
	}
}

func TestLIFO(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)

	cases := []string{"a", "b", "c"}
	for _, msg := range cases {
		h, err := sh.Start("lifo", nil)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h.Stdin(), "%s\n", msg)
	}
	var buf bytes.Buffer
	if err := sh.Cleanup(&buf, nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if got, want := len(lines), len(cases); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(cases)))
	for i, msg := range cases {
		if got, want := lines[i], fmt.Sprintf("%p: %s", lifo, msg); got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestStartOpts(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := modules.StartOpts{
		External: true,
	}
	sh.SetDefaultStartOpts(opts)
	def := sh.DefaultStartOpts()
	if got, want := def.External, opts.External; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	def.External = false
	if got, want := def, (modules.StartOpts{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Verify that the shell retains a copy.
	opts.External = false
	opts.ExecProtocol = true
	def = sh.DefaultStartOpts()
	if got, want := def.External, true; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := def.ExecProtocol, false; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	sh.SetDefaultStartOpts(opts)
	def = sh.DefaultStartOpts()
	if got, want := def.ExecProtocol, true; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEmbeddedSession(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	def := sh.DefaultStartOpts()
	if def.ExpectTesting == nil {
		t.Fatalf("ExpectTesting should be non nil")
	}
}

func TestCredentialsAndNoExec(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts = opts.NoExecCommand()
	creds, err := sh.NewCustomCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts = opts.WithCustomCredentials(creds)
	h, err := sh.StartWithOpts(opts, nil, "echos", "a")

	if got, want := err, modules.ErrNoExecAndCustomCreds; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := h, modules.Handle(nil); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
