package exec_test

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	vexec "v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/exec/consts"
	// Use mock timekeeper to avoid actually sleeping during the test.
	"v.io/core/veyron/lib/testutil/timekeeper"
)

// We always expect there to be exactly three open file descriptors
// when the test starts out: STDIN, STDOUT, and STDERR. This
// assumption is tested in init below, and in the rare cases where it
// is wrong, we try to close all additional file descriptors, and bail
// out if that fails.
const baselineOpenFiles = 3

func init() {
	if os.Getenv("GO_WANT_HELPER_PROCESS_EXEC") == "1" {
		return
	}
	want, got := baselineOpenFiles, openFiles()
	if want == got {
		return
	}
	for i := want; i < got; i++ {
		syscall.Close(i)
	}
	if want, got = baselineOpenFiles, openFiles(); want != got {
		message := `Test expected to start with %d open files, found %d instead.
This can happen if parent process has any open file descriptors,
e.g. pipes, that are being inherited.`
		panic(fmt.Errorf(message, want, got))
	}
}

// These tests need to run a subprocess and we reuse this same test
// binary to do so. A fake test 'TestHelperProcess' contains the code
// we need to run in the child and we simply run this same binary with
// a test.run= arg that runs just that test. This idea was taken from
// the tests for os/exec.
func helperCommand(s ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--"}
	cs = append(cs, s...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append([]string{"GO_WANT_HELPER_PROCESS_EXEC=1"}, os.Environ()...)
	return cmd
}

func openFiles() int {
	f, err := os.Open("/dev/null")
	if err != nil {
		panic("Failed to open /dev/null\n")
	}
	n := f.Fd()
	f.Close()
	return int(n)
}

func clean(t *testing.T, ph ...*vexec.ParentHandle) {
	for _, p := range ph {
		alreadyClean := !p.Exists()
		p.Clean()
		if !alreadyClean && p.Exists() {
			t.Errorf("child process left behind even after calling Clean")
		}
	}
	if want, got := baselineOpenFiles, openFiles(); want != got {
		t.Errorf("Leaking file descriptors: expect %d, got %d", want, got)
	}
}

func read(ch chan bool, r io.Reader, m string) {
	buf := make([]byte, 4096*4)
	n, err := r.Read(buf)
	if err != nil {
		log.Printf("failed to read message: error %s, expecting '%s'\n",
			err, m)
		ch <- false
		return
	}
	g := string(buf[:n])
	b := g == m
	if !b {
		log.Printf("read '%s', not '%s'\n", g, m)
	}
	ch <- b
}

func expectMessage(r io.Reader, m string) bool {
	ch := make(chan bool, 1)
	go read(ch, r, m)
	select {
	case b := <-ch:
		return b
	case <-time.After(5 * time.Second):
		log.Printf("expectMessage: timeout\n")
		return false
	}
	panic("unreachable")
}

func TestConfigExchange(t *testing.T) {
	cmd := helperCommand("testConfig")
	stderr, _ := cmd.StderrPipe()
	cfg := vexec.NewConfig()
	cfg.Set("foo", "bar")
	ph := vexec.NewParentHandle(cmd, vexec.ConfigOpt{cfg})
	err := ph.Start()
	if err != nil {
		t.Fatalf("testConfig: start: %v", err)
	}
	serialized, err := cfg.Serialize()
	if err != nil {
		t.Fatalf("testConfig: failed to serialize config: %v", err)
	}
	if !expectMessage(stderr, serialized) {
		t.Errorf("unexpected output from child")
	} else {
		if err = cmd.Wait(); err != nil {
			t.Errorf("testConfig: wait: %v", err)
		}
	}
	clean(t, ph)
}

func TestSecretExchange(t *testing.T) {
	cmd := helperCommand("testSecret")
	stderr, _ := cmd.StderrPipe()
	ph := vexec.NewParentHandle(cmd, vexec.SecretOpt("dummy_secret"))
	err := ph.Start()
	if err != nil {
		t.Fatalf("testSecretTest: start: %v", err)
	}
	if !expectMessage(stderr, "dummy_secret") {
		t.Errorf("unexpected output from child")
	} else {
		if err = cmd.Wait(); err != nil {
			t.Errorf("testSecretTest: wait: %v", err)
		}
	}
	clean(t, ph)
}

func TestNoVersion(t *testing.T) {
	// Make sure that Init correctly tests for the presence of VEXEC_VERSION
	_, err := vexec.GetChildHandle()
	if err != vexec.ErrNoVersion {
		t.Errorf("Should be missing Version")
	}
}

func waitForReady(t *testing.T, cmd *exec.Cmd, name string, delay int, ph *vexec.ParentHandle) error {
	err := ph.Start()
	if err != nil {
		t.Fatalf("%s: start: %v", name, err)
		return err
	}
	return ph.WaitForReady(time.Duration(delay) * time.Second)
}

func readyHelperCmd(t *testing.T, cmd *exec.Cmd, name, result string) *vexec.ParentHandle {
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("%s: failed to get stderr pipe\n", name)
	}
	ph := vexec.NewParentHandle(cmd)
	if err := waitForReady(t, cmd, name, 4, ph); err != nil {
		t.Errorf("%s: WaitForReady: %v (%v)", name, err, ph)
		return nil
	}
	if !expectMessage(stderr, result) {
		t.Errorf("%s: failed to read '%s' from child\n", name, result)
	}
	return ph
}

func readyHelper(t *testing.T, name, test, result string) *vexec.ParentHandle {
	cmd := helperCommand(test)
	return readyHelperCmd(t, cmd, name, result)
}

func testMany(t *testing.T, name, test, result string) []*vexec.ParentHandle {
	nprocs := 10
	ph := make([]*vexec.ParentHandle, nprocs)
	cmd := make([]*exec.Cmd, nprocs)
	stderr := make([]io.ReadCloser, nprocs)
	controlReaders := make([]io.ReadCloser, nprocs)
	var done sync.WaitGroup
	for i := 0; i < nprocs; i++ {
		cmd[i] = helperCommand(test)
		// The control pipe is used to signal the child when to wake up.
		controlRead, controlWrite, err := os.Pipe()
		if err != nil {
			t.Errorf("Failed to create control pipe: %v", err)
			return nil
		}
		controlReaders[i] = controlRead
		cmd[i].ExtraFiles = append(cmd[i].ExtraFiles, controlRead)
		stderr[i], _ = cmd[i].StderrPipe()
		tk := timekeeper.NewManualTime()
		ph[i] = vexec.NewParentHandle(cmd[i], vexec.TimeKeeperOpt{tk})
		done.Add(1)
		go func() {
			// For simulated slow children, wait until the parent
			// starts waiting, and then wake up the child.
			if test == "testReadySlow" {
				<-tk.Requests()
				tk.AdvanceTime(3 * time.Second)
				if _, err = controlWrite.Write([]byte("wake")); err != nil {
					t.Errorf("Failed to write to control pipe: %v", err)
				}
			}
			controlWrite.Close()
			done.Done()
		}()
		if err := ph[i].Start(); err != nil {
			t.Errorf("%s: Failed to start child %d: %s\n", name, i, err)
		}
	}
	for i := 0; i < nprocs; i++ {
		if err := ph[i].WaitForReady(5 * time.Second); err != nil {
			t.Errorf("%s: Failed to wait for child %d: %s\n", name, i, err)
		}
		controlReaders[i].Close()
	}
	for i := 0; i < nprocs; i++ {
		if !expectMessage(stderr[i], result) {
			t.Errorf("%s: Failed to read message from child %d\n", name, i)
		}
	}
	done.Wait()
	return ph
}

func TestToReadyMany(t *testing.T) {
	clean(t, testMany(t, "TestToReadyMany", "testReady", ".")...)
}

func TestToReadySlowMany(t *testing.T) {
	clean(t, testMany(t, "TestToReadySlowMany", "testReadySlow", "..")...)
}

func TestToReady(t *testing.T) {
	ph := readyHelper(t, "TestToReady", "testReady", ".")
	clean(t, ph)
}

func TestToFail(t *testing.T) {
	name := "testFail"
	cmd := helperCommand(name, "failed", "to", "start")
	ph := vexec.NewParentHandle(cmd)
	err := waitForReady(t, cmd, name, 4, ph)
	if err == nil || err.Error() != "failed to start" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToFailInvalidUTF8(t *testing.T) {
	name := "testFail"
	cmd := helperCommand(name, "invalid", "utf8", string([]byte{0xFF}), "in", string([]byte{0xFC}), "error", "message")
	ph := vexec.NewParentHandle(cmd)
	err := waitForReady(t, cmd, name, 4, ph)
	if err == nil || err.Error() != "invalid utf8 "+string(utf8.RuneError)+" in "+string(utf8.RuneError)+" error message" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNeverReady(t *testing.T) {
	name := "testNeverReady"
	result := "never ready"
	cmd := helperCommand(name)
	stderr, _ := cmd.StderrPipe()
	ph := vexec.NewParentHandle(cmd)
	err := waitForReady(t, cmd, name, 1, ph)
	if err != vexec.ErrTimeout {
		t.Errorf("Failed to get timeout: got %v\n", err)
	} else {
		// block waiting for error from child
		if !expectMessage(stderr, result) {
			t.Errorf("%s: failed to read '%s' from child\n", name, result)
		}
	}
	clean(t, ph)
}

func TestTooSlowToReady(t *testing.T) {
	name := "testTooSlowToReady"
	result := "write status_wr: broken pipe"
	cmd := helperCommand(name)
	// The control pipe is used to signal the child when to wake up.
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Errorf("Failed to create control pipe: %v", err)
		return
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, controlRead)
	stderr, _ := cmd.StderrPipe()
	tk := timekeeper.NewManualTime()
	ph := vexec.NewParentHandle(cmd, vexec.TimeKeeperOpt{tk})
	defer clean(t, ph)
	defer controlWrite.Close()
	defer controlRead.Close()
	// Wait for the parent to start waiting, then simulate a timeout.
	go func() {
		toWait := <-tk.Requests()
		tk.AdvanceTime(toWait)
	}()
	err = waitForReady(t, cmd, name, 1, ph)
	if err != vexec.ErrTimeout {
		t.Errorf("Failed to get timeout: got %v\n", err)
	} else {
		// After the parent timed out, wake up the child and let it
		// proceed.
		if _, err = controlWrite.Write([]byte("wake")); err != nil {
			t.Errorf("Failed to write to control pipe: %v", err)
		} else {
			// block waiting for error from child
			if !expectMessage(stderr, result) {
				t.Errorf("%s: failed to read '%s' from child\n", name, result)
			}
		}
	}
}

func TestToReadySlow(t *testing.T) {
	name := "TestToReadySlow"
	cmd := helperCommand("testReadySlow")
	// The control pipe is used to signal the child when to wake up.
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Errorf("Failed to create control pipe: %v", err)
		return
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, controlRead)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("%s: failed to get stderr pipe", name)
	}
	tk := timekeeper.NewManualTime()
	ph := vexec.NewParentHandle(cmd, vexec.TimeKeeperOpt{tk})
	defer clean(t, ph)
	defer controlWrite.Close()
	defer controlRead.Close()
	// Wait for the parent to start waiting, simulate a short wait (but not
	// enough to timeout the parent), then wake up the child.
	go func() {
		<-tk.Requests()
		tk.AdvanceTime(2 * time.Second)
		if _, err = controlWrite.Write([]byte("wake")); err != nil {
			t.Errorf("Failed to write to control pipe: %v", err)
		}
	}()
	if err := waitForReady(t, cmd, name, 4, ph); err != nil {
		t.Errorf("%s: WaitForReady: %v (%v)", name, err, ph)
		return
	}
	// After the child has replied, simulate a timeout on the server by
	// advacing the time more; at this point, however, the timeout should no
	// longer occur since the child already replied.
	tk.AdvanceTime(2 * time.Second)
	if result := ".."; !expectMessage(stderr, result) {
		t.Errorf("%s: failed to read '%s' from child\n", name, result)
	}
}

func TestToCompletion(t *testing.T) {
	ph := readyHelper(t, "TestToCompletion", "testSuccess", "...ok")
	e := ph.Wait(time.Second)
	if e != nil {
		t.Errorf("Wait failed: err %s\n", e)
	}
	clean(t, ph)
}

func TestToCompletionError(t *testing.T) {
	ph := readyHelper(t, "TestToCompletionError", "testError", "...err")
	e := ph.Wait(time.Second)
	if e == nil {
		t.Errorf("Wait failed: err %s\n", e)
	}
	clean(t, ph)
}

func TestExtraFiles(t *testing.T) {
	cmd := helperCommand("testExtraFiles")
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %s\n", err)
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, rd)
	msg := "hello there..."
	fmt.Fprintf(wr, msg)
	ph := readyHelperCmd(t, cmd, "TestExtraFiles", "child: "+msg)
	if ph == nil {
		t.Fatalf("Failed to get vexec.ParentHandle\n")
	}
	e := ph.Wait(1 * time.Second)
	if e != nil {
		t.Errorf("Wait failed: err %s\n", e)
	}
	rd.Close()
	wr.Close()
	clean(t, ph)
}

func TestExitEarly(t *testing.T) {
	name := "exitEarly"
	cmd := helperCommand(name)
	tk := timekeeper.NewManualTime()
	ph := vexec.NewParentHandle(cmd, vexec.TimeKeeperOpt{tk})
	err := ph.Start()
	if err != nil {
		t.Fatalf("%s: start: %v", name, err)
	}
	e := ph.Wait(time.Second)
	if e == nil || e.Error() != "exit status 1" {
		t.Errorf("Unexpected value for error: %v\n", e)
	}
	clean(t, ph)
}

func TestWaitAndCleanRace(t *testing.T) {
	name := "testReadyAndHang"
	cmd := helperCommand(name)
	tk := timekeeper.NewManualTime()
	ph := vexec.NewParentHandle(cmd, vexec.TimeKeeperOpt{tk})
	if err := waitForReady(t, cmd, name, 1, ph); err != nil {
		t.Errorf("%s: WaitForReady: %v (%v)", name, err, ph)
	}
	go func() {
		// Wait for the ph.Wait below to block, then advance the time
		// s.t. the Wait times out.
		<-tk.Requests()
		tk.AdvanceTime(2 * time.Second)
	}()
	if got, want := ph.Wait(time.Second), vexec.ErrTimeout; got == nil || got.Error() != want.Error() {
		t.Errorf("Wait returned %v, wanted %v instead", got, want)
	}
	if got, want := ph.Clean(), "signal: killed"; got == nil || got.Error() != want {
		t.Errorf("Wait returned %v, wanted %v instead", got, want)
	}
}

func verifyNoExecVariable() {
	version := os.Getenv(consts.ExecVersionVariable)
	if len(version) != 0 {
		log.Fatalf("Version variable %q has a value: %s", consts.ExecVersionVariable, version)
	}
}

// TestHelperProcess isn't a real test; it's used as a helper process
// for the other tests.
func TestHelperProcess(*testing.T) {
	// Return immediately if this is not run as the child helper
	// for the other tests.
	if os.Getenv("GO_WANT_HELPER_PROCESS_EXEC") != "1" {
		return
	}
	defer os.Exit(0)

	version := os.Getenv(consts.ExecVersionVariable)
	if len(version) == 0 {
		log.Fatalf("Version variable %q has no value", consts.ExecVersionVariable)
	}

	// Write errors to stderr or using log. since the parent
	// process is reading stderr.
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		log.Fatal("No command")
	}

	cmd, args := args[0], args[1:]

	switch cmd {
	case "exitEarly":
		_, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(1)
	case "testNeverReady":
		_, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		fmt.Fprintf(os.Stderr, "never ready")
	case "testTooSlowToReady":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		// Wait for the parent to tell us when it's ok to proceed.
		controlPipe := ch.NewExtraFile(0, "control_rd")
		for {
			buf := make([]byte, 100)
			n, err := controlPipe.Read(buf)
			if err != nil {
				log.Fatal(err)
			}
			if n > 0 {
				break
			}
		}
		// SetReady should return an error since the parent has
		// timed out on us and we'd be writing to a closed pipe.
		if err := ch.SetReady(); err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
		} else {
			fmt.Fprintf(os.Stderr, "didn't get the expected error")
		}
	case "testFail":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		err = ch.SetFailed(fmt.Errorf("%s", strings.Join(args, " ")))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
		// It's fine to call SetFailed multiple times.
		if err2 := ch.SetFailed(fmt.Errorf("dummy")); err != err2 {
			fmt.Fprintf(os.Stderr, "Received new error got: %v, want %v\n", err2, err)
		}
	case "testReady":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		err = ch.SetReady()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
		// It's fine to call SetReady multiple times.
		if err2 := ch.SetReady(); err != err2 {
			fmt.Fprintf(os.Stderr, "Received new error got: %v, want %v\n", err2, err)
		}
		fmt.Fprintf(os.Stderr, ".")
	case "testReadySlow":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		// Wait for the parent to tell us when it's ok to proceed.
		controlPipe := ch.NewExtraFile(0, "control_rd")
		for {
			buf := make([]byte, 100)
			n, err := controlPipe.Read(buf)
			if err != nil {
				log.Fatal(err)
			}
			if n > 0 {
				break
			}
		}
		ch.SetReady()
		fmt.Fprintf(os.Stderr, "..")
	case "testReadyAndHang":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		ch.SetReady()
		<-time.After(time.Minute)
	case "testSuccess", "testError":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatal(err)
		}
		verifyNoExecVariable()
		ch.SetReady()
		rc := make(chan int)
		go func() {
			if cmd == "testError" {
				fmt.Fprintf(os.Stderr, "...err")
				rc <- 1
			} else {
				fmt.Fprintf(os.Stderr, "...ok")
				rc <- 0
			}
		}()
		r := <-rc
		os.Exit(r)
	case "testConfig":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatalf("%v", err)
		} else {
			verifyNoExecVariable()
			serialized, err := ch.Config.Serialize()
			if err != nil {
				log.Fatalf("%v", err)
			}
			fmt.Fprintf(os.Stderr, "%s", serialized)
		}
	case "testSecret":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatalf("%s", err)
		} else {
			verifyNoExecVariable()
			fmt.Fprintf(os.Stderr, "%s", ch.Secret)
		}
	case "testExtraFiles":
		ch, err := vexec.GetChildHandle()
		if err != nil {
			log.Fatalf("error.... %s\n", err)
		}
		verifyNoExecVariable()
		err = ch.SetReady()
		rd := ch.NewExtraFile(0, "read")
		buf := make([]byte, 1024)
		if n, err := rd.Read(buf); err != nil {
			fmt.Fprintf(os.Stderr, "child: error %s\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "child: %s", string(buf[:n]))
		}
	}
}
