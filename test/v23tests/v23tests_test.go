// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package v23tests_test

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/security"

	"v.io/x/ref"
	"v.io/x/ref/internal/logger"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23tests"
)

func TestBinaryFromPath(t *testing.T) {
	env := v23tests.New(t)
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
	env := v23tests.New(t)
	defer env.Cleanup()

	v23tests.RunRootMT(env, "--v23.tcp.address=127.0.0.1:0")
	proxyBin := env.BuildV23Pkg("v.io/x/ref/services/proxy/proxyd")
	nsBin := env.BuildGoPkg("v.io/x/ref/cmd/namespace")

	mt, ok := env.GetVar(ref.EnvNamespacePrefix)
	if !ok || len(mt) == 0 {
		t.Fatalf("expected a mount table name")
	}

	proxy := proxyBin.Start("--v23.tcp.address=127.0.0.1:0", "-name=proxyd")
	proxyName := proxy.ExpectVar("NAME")
	proxyAddress, _ := naming.SplitAddressName(proxyName)

	re := regexp.MustCompile("proxyd (.*) \\(.*")
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		inv := nsBin.Start("glob", "...")
		line, _ := inv.ReadAll()
		parts := re.FindStringSubmatch(line)
		if len(parts) == 2 {
			if want, got := security.JoinPatternName("root/child", proxyAddress), parts[1]; got != want {
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
func IntegrationTestInChild(i *v23tests.T) {
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
	if modules.IsChildProcess() {
		globalT = t
		if err := modules.Dispatch(); err != nil {
			t.Errorf("modules.Dispatch failed: %v", err)
		}
	}
}

var RunIntegrationTestInChild = modules.Register(func(env *modules.Env, args ...string) error {
	v23tests.RunTest(globalT, IntegrationTestInChild)
	return nil
}, "RunIntegrationTestInChild")

func TestDeferHandling(t *testing.T) {
	t.Skip("https://v.io/i/686 -- test is flaky in Go1.5")
	sh, _ := modules.NewShell(nil, nil, testing.Verbose(), t)
	child, err := sh.Start(nil, RunIntegrationTestInChild, "--test.run=TestHelperProcess", "--v23.tests")
	if err != nil {
		t.Fatal(err)
	}
	child.Expect("Hello")
	child.ExpectRE("--- FAIL: TestHelperProcess", -1)
	for _, e := range []string{
		".* 0: /bin/sleep: shutdown status: has not been shutdown",
		".* 1: /bin/sleep: shutdown status: signal: terminated",
		".* 2: /bin/sleep: shutdown status: has not been shutdown",
	} {
		child.ExpectRE(e, -1)
	}
	var stderr bytes.Buffer
	if err := child.Shutdown(nil, &stderr); err != nil {
		if !strings.Contains(err.Error(), "exit status 1") {
			t.Fatal(err)
		}
	}
	logger.Global().Infof("Child\n=============\n%s", stderr.String())
	logger.Global().Infof("-----------------")
}

func TestInputRedirection(t *testing.T) {
	testutil.InitRandGenerator(t.Logf)
	env := v23tests.New(t)
	defer env.Cleanup()

	echo := env.BinaryFromPath("/bin/echo")
	cat := env.BinaryFromPath("/bin/cat")

	if want, got := "Hello, world!\n", cat.WithStdin(echo.Start("Hello, world!").Stdout()).Start().Output(); want != got {
		t.Fatalf("unexpected output, got %q, want %q", got, want)
	}

	// Read something from a file.
	{
		want := "Hello from a file!"
		f := env.NewTempFile()
		f.WriteString(want)
		f.Seek(0, 0)
		if got := cat.WithStdin(f).Start().Output(); want != got {
			t.Fatalf("unexpected output, got %q, want %q", got, want)
		}
	}

	// Try it again with 1Mb.
	{
		want := testutil.RandomBytes(1 << 20)
		expectedSum := sha1.Sum(want)
		f := env.NewTempFile()
		f.Write(want)
		f.Seek(0, 0)
		got := cat.WithStdin(f).Start().Output()
		if len(got) != len(want) {
			t.Fatalf("length mismatch, got %d but wanted %d", len(want), len(got))
		}
		actualSum := sha1.Sum([]byte(got))
		if actualSum != expectedSum {
			t.Fatalf("SHA-1 mismatch, got %x but wanted %x", actualSum, expectedSum)
		}
	}
}

func TestDirStack(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	home := os.Getenv("HOME")
	if len(home) == 0 {
		t.Fatalf("failed to read HOME environment variable")
	}

	getwd := func() string {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd() failed: %v", err)
		}
		return cwd
	}

	cwd := getwd()
	if got, want := env.Pushd("/"), cwd; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := env.Pushd(home), "/"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	tcwd := getwd()
	if got, want := tcwd, home; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := env.Popd(), "/"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := env.Popd(), cwd; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	ncwd := getwd()
	if got, want := ncwd, cwd; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRun(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	if got, want := env.Run("/bin/echo", "hello world"), "hello world"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	echo := env.BinaryFromPath("/bin/echo")
	if got, want := echo.Run("hello", "world"), "hello world"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	sadEcho := echo.WithPrefixArgs("sad")
	if got, want := sadEcho.Run("hello", "world"), "sad hello world"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}

	happyEcho := echo.WithPrefixArgs("happy")
	if got, want := happyEcho.Run("hello", "world"), "happy hello world"; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

type mockT struct {
	msg    string
	failed bool
}

func (m *mockT) Error(args ...interface{}) {
	m.msg = fmt.Sprint(args...)
	m.failed = true
}

func (m *mockT) Errorf(format string, args ...interface{}) {
	m.msg = fmt.Sprintf(format, args...)
	m.failed = true
}

func (m *mockT) Fail() { panic("Fail") }

func (m *mockT) FailNow() { panic("FailNow") }

func (m *mockT) Failed() bool { return m.failed }

func (m *mockT) Fatal(args ...interface{}) {
	panic(fmt.Sprint(args...))
}

func (m *mockT) Fatalf(format string, args ...interface{}) {
	panic(fmt.Sprintf(format, args...))
}

func (m *mockT) Log(args ...interface{}) {}

func (m *mockT) Logf(format string, args ...interface{}) {}

func (m *mockT) Skip(args ...interface{}) {}

func (m *mockT) SkipNow() {}

func (m *mockT) Skipf(format string, args ...interface{}) {}

func (m *mockT) Skipped() bool { return false }

func TestRunFailFromPath(t *testing.T) {
	mock := &mockT{}
	env := v23tests.New(mock)
	defer env.Cleanup()

	defer func() {
		msg := recover().(string)
		// this, and the tests below are intended to ensure that line #s
		// are captured and reported correctly.
		if got, want := msg, "v23tests_test.go:304"; !strings.Contains(got, want) {
			t.Fatalf("%q does not contain %q", got, want)
		}
		if got, want := msg, "fork/exec /bin/echox: no such file or directory"; !strings.Contains(got, want) {
			t.Fatalf("%q does not contain %q", got, want)
		}
	}()
	env.Run("/bin/echox", "hello world")
}

func TestRunFail(t *testing.T) {
	mock := &mockT{}
	env := v23tests.New(mock)
	defer env.Cleanup()

	// Fail fast.
	sh := env.Shell()
	opts := sh.DefaultStartOpts()
	opts.StartTimeout = 100 * time.Millisecond
	sh.SetDefaultStartOpts(opts)
	defer func() {
		msg := recover().(string)
		if got, want := msg, "v23tests_test.go:326"; !strings.Contains(got, want) {
			t.Fatalf("%q does not contain %q", got, want)
		}
		if got, want := msg, "StartWithOpts"; !strings.Contains(got, want) {
			t.Fatalf("%q does not contain %q", got, want)
		}
	}()
	v23tests.RunRootMT(env, "--xxv23.tcp.address=127.0.0.1:0")
}

func TestWaitTimeout(t *testing.T) {
	env := v23tests.New(&mockT{})
	defer env.Cleanup()

	iterations := 0
	sleeper := func() (interface{}, error) {
		iterations++
		return nil, nil
	}

	defer func() {
		if iterations == 0 {
			t.Fatalf("our sleeper didn't get to run")
		}
		if got, want := recover().(string), "v23tests_test.go:347: timed out"; !strings.Contains(got, want) {
			t.Fatalf("%q does not contain %q", got, want)
		}
	}()
	env.WaitFor(sleeper, time.Millisecond, 50*time.Millisecond)
}

func TestWaitAsyncTimeout(t *testing.T) {
	env := v23tests.New(&mockT{})
	defer env.Cleanup()

	iterations := 0
	sleeper := func() (interface{}, error) {
		time.Sleep(time.Minute)
		iterations++
		return nil, nil
	}

	defer func() {
		if iterations != 0 {
			t.Fatalf("our sleeper got to run")
		}
		if got, want := recover().(string), "v23tests_test.go:369: timed out"; !strings.Contains(got, want) {
			t.Fatalf("%q does not contain %q", got, want)
		}
	}()
	env.WaitForAsync(sleeper, time.Millisecond, 50*time.Millisecond)
}

func TestWaitFor(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()
	iterations := 0
	countIn5s := func() (interface{}, error) {
		iterations++
		if iterations%5 == 0 {
			return iterations, nil
		}
		return nil, nil
	}

	r := env.WaitFor(countIn5s, time.Millisecond, 50*time.Millisecond)
	if got, want := r.(int), 5; got != want {
		env.Fatalf("got %d, want %d", got, want)
	}

	r = env.WaitForAsync(countIn5s, time.Millisecond, 50*time.Millisecond)
	if got, want := r.(int), 10; got != want {
		env.Fatalf("got %d, want %d", got, want)
	}
}

func builder(t *testing.T) (string, string) {
	env := v23tests.New(t)
	defer env.Cleanup()
	bin := env.BuildGoPkg("v.io/x/ref/test/v23tests")
	return env.BinDir(), bin.Path()
}

func TestCachedBuild(t *testing.T) {
	cleanup := v23tests.UseSharedBinDir()
	defer cleanup()
	defer os.Setenv("V23_BIN_DIR", "")

	bin1, path1 := builder(t)
	bin2, path2 := builder(t)

	if bin1 != bin2 {
		t.Fatalf("caching failed, bin dirs differ: %q != %q", bin1, bin2)
	}

	if path1 != path2 {
		t.Fatalf("caching failed, paths differ: %q != %q", path1, path2)
	}
}

func TestUncachedBuild(t *testing.T) {
	bin1, path1 := builder(t)
	bin2, path2 := builder(t)

	if bin1 == bin2 {
		t.Fatalf("failed, bin dirs are the same: %q != %q", bin1, bin2)
	}

	if path1 == path2 {
		t.Fatalf("failed, paths are the same: %q != %q", path1, path2)
	}
}

func TestShutdownAndCleanupTogetherDontHang(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	bash := env.BinaryFromPath("/bin/bash")
	if want, got := "hello world\n", bash.Start("-c", "echo hello world").Output(); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}

	inv := bash.Start("-c", "echo hello world")
	var buf bytes.Buffer
	inv.Shutdown(&buf, nil)
	if want, got := "hello world\n", buf.String(); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}
	// Make sure that we can call Shutdown and Cleanup without hanging.
}
