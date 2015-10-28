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
	"v.io/x/ref"
	"v.io/x/ref/lib/exec"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"

	_ "v.io/x/ref/runtime/factories/generic"
)

// We must call TestMain ourselves because using jiri test generate
// creates an import cycle for this package.
func TestMain(m *testing.M) {
	modules.DispatchAndExitIfChild()
	os.Exit(m.Run())
}

var ignoreStdin = modules.Register(func(*modules.Env, ...string) error {
	<-time.After(time.Minute)
	return nil
}, "ignoreStdin")

var echo = modules.Register(func(env *modules.Env, args ...string) error {
	for _, a := range args {
		fmt.Fprintf(env.Stdout, "stdout: %s\n", a)
		fmt.Fprintf(env.Stderr, "stderr: %s\n", a)
	}
	return nil
}, "Echo")

var pipeEcho = modules.Register(pipeEchoFunc, "pipeEcho")

func pipeEchoFunc(env *modules.Env, args ...string) error {
	scanner := bufio.NewScanner(env.Stdin)
	for scanner.Scan() {
		fmt.Fprintf(env.Stdout, "%p: %s\n", pipeEchoFunc, scanner.Text())
	}
	return nil
}

var lifo = modules.Register(lifoFunc, "lifo")

func lifoFunc(env *modules.Env, args ...string) error {
	scanner := bufio.NewScanner(env.Stdin)
	scanner.Scan()
	msg := scanner.Text()
	modules.WaitForEOF(env.Stdin)
	fmt.Fprintf(env.Stdout, "%p: %s\n", lifoFunc, msg)
	return nil
}

var printBlessing = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	blessing := v23.GetPrincipal(ctx).BlessingStore().Default()
	fmt.Fprintf(env.Stdout, "%s", blessing)
	return nil
}, "printBlessing")

var envTest = modules.Register(func(env *modules.Env, args ...string) error {
	for _, a := range args {
		if v := env.Vars[a]; len(v) > 0 {
			fmt.Fprintf(env.Stdout, "%s\n", a+"="+v)
		} else {
			fmt.Fprintf(env.Stderr, "missing %s\n", a)
		}
	}
	modules.WaitForEOF(env.Stdin)
	fmt.Fprintf(env.Stdout, "done\n")
	return nil
}, "envTest")

const printEnvArgPrefix = "PRINTENV_ARG="

var printEnv = modules.Register(func(env *modules.Env, args ...string) error {
	for _, a := range args {
		fmt.Fprintf(env.Stdout, "%s%s\n", printEnvArgPrefix, a)
	}
	for k, v := range env.Vars {
		fmt.Fprintf(env.Stdout, "%q\n", k+"="+v)
	}
	return nil
}, "printEnv")

var errorMain = modules.Register(func(env *modules.Env, args ...string) error {
	return fmt.Errorf("an error")
}, "errorMain")

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

func testProgram(t *testing.T, sh *modules.Shell, prog modules.Program, key, val string) {
	h, err := sh.Start(nil, prog, key)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer func() {
		var stdout, stderr bytes.Buffer
		sh.Cleanup(&stdout, &stderr)
		want := ""
		if testing.Verbose() {
			want = "---- Shell Cleanup ----\n---- Cleanup calling cancelCtx ----\n---- Shell Cleanup Complete ----\n"
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
	h, err := sh.Start(env, printBlessing)
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
	h, err := sh.StartWithOpts(sh.DefaultStartOpts().WithCustomCredentials(creds), nil, printBlessing)
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
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testProgram(t, sh, envTest, key, val)
}

func TestAgent(t *testing.T) {
	ctx, shutdown := test.V23Init()
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
	ctx, shutdown := test.V23Init()
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
	ctx, shutdown := test.V23Init()
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
	if got, want := getBlessing(t, sh, fmt.Sprintf("%s=%s", ref.EnvCredentials, creds)), noagent; got != want {
		t.Errorf("Bad blessing. Got %q, want %q", got, want)
	}
}

func TestChildNoRegistration(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	key, val := "simpleVar", "foo & bar"
	sh.SetVar(key, val)
	testProgram(t, sh, envTest, key, val)
	_, err = sh.Start(nil, modules.Program("non-existent-program"), "random", "args")
	if err == nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		t.Fatalf("expected error")
	}
}

func TestErrorChild(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start(nil, errorMain)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := h.Shutdown(nil, nil), "exit status 1"; got == nil || got.Error() != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func testShutdown(t *testing.T, sh *modules.Shell, prog modules.Program) {
	result := ""
	args := []string{"a", "b c", "ddd"}
	if _, err := sh.Start(nil, prog, args...); err != nil {
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
	if got, want := stderrBuf.String(), stderrOutput; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestShutdownSubprocess(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, false, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	testShutdown(t, sh, echo)
}

// TestShutdownSubprocessIgnoreStdin verifies that Shutdown doesn't wait
// forever if a child does not die upon closing stdin; but instead times out and
// returns an appropriate error.
func TestShutdownSubprocessIgnoreStdin(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, false, t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts.ShutdownTimeout = time.Second
	h, err := sh.StartWithOpts(opts, nil, ignoreStdin)
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
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts.ShutdownTimeout = time.Second
	h, err := sh.StartWithOpts(opts, nil, ignoreStdin)
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

func find(want string, in []string) bool {
	for _, a := range in {
		if a == want {
			return true
		}
	}
	return false
}

func TestEnvelope(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	sh.SetVar("a", "1")
	sh.SetVar("b", "2")
	args := []string{"oh", "ah"}
	h, err := sh.Start(nil, printEnv, args...)
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
	shArgs, shEnv := sh.ProgramEnvelope(nil, printEnv, args...)
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
		if want == "\""+exec.ExecVersionVariable+"=\"" {
			continue
		}
		if !find(want, shEnv) {
			t.Errorf("failed to find %s in %#v", want, shEnv)
		}
	}
}

func TestEnvMerge(t *testing.T) {
	ctx, shutdown := test.V23Init()
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
	h, err := sh.Start([]string{"b=2"}, printEnv)
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

func TestNoExec(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.StartWithOpts(sh.DefaultStartOpts().NoExecProgram(), nil, modules.Program("/bin/echo"), "hello", "world")
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
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	cookie := strconv.Itoa(rand.Int())
	sh.SetConfigKey("cookie", cookie)
	h, err := sh.StartWithOpts(sh.DefaultStartOpts().ExternalProgram(), nil, modules.Program(os.Args[0]), "--test.run=TestExternalTestHelper")
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
	ctx, shutdown := test.V23Init()
	defer shutdown()

	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	opts := sh.DefaultStartOpts()
	opts.Stdin = r
	h, err := sh.StartWithOpts(opts, nil, pipeEcho)
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
		fmt.Sprintf("%p: hello world", pipeEchoFunc),
		fmt.Sprintf("%p: %s", pipeEchoFunc, cookie),
	}
	i := 0
	for scanner.Scan() {
		if got, want := scanner.Text(), want[i]; got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		i++
	}
	if got, want := i, 2; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := h.Shutdown(os.Stderr, os.Stderr); err != nil {
		t.Fatal(err)
	}
	r.Close()
}

func TestLIFO(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)

	cases := []string{"a", "b", "c"}
	for _, msg := range cases {
		h, err := sh.Start(nil, lifo)
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
		if got, want := lines[i], fmt.Sprintf("%p: %s", lifoFunc, msg); got != want {
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
	ctx, shutdown := test.V23Init()
	defer shutdown()
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts := sh.DefaultStartOpts()
	opts = opts.NoExecProgram()
	creds, err := sh.NewCustomCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	opts = opts.WithCustomCredentials(creds)
	h, err := sh.StartWithOpts(opts, nil, echo, "a")

	if got, want := err, modules.ErrNoExecAndCustomCreds; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := h, modules.Handle(nil); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
