package rt_test

import (
	"fmt"
	"syscall"
	"testing"

	"veyron2"
	"veyron2/rt"

	"veyron/lib/testutil/blackbox"
)

func init() {
	blackbox.CommandTable["withRuntime"] = withRuntime
	blackbox.CommandTable["withoutRuntime"] = withoutRuntime
}

func simpleEchoProgram() {
	fmt.Println("ready")
	fmt.Println(blackbox.ReadLineFromStdin())
	blackbox.WaitForEOFOnStdin()
}

func withRuntime([]string) {
	// Make sure that we use "google" runtime implementation in this
	// package even though we have to use the public API which supports
	// arbitrary runtime implementations.
	rt.Init(veyron2.RuntimeOpt{veyron2.GoogleRuntimeName})
	simpleEchoProgram()
}

func withoutRuntime([]string) {
	simpleEchoProgram()
}

func TestWithRuntime(t *testing.T) {
	c := blackbox.HelperCommand(t, "withRuntime")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGHUP)
	c.WriteLine("foo")
	c.Expect("foo")
	c.CloseStdin()
	c.ExpectEOFAndWait()
}

func TestWithoutRuntime(t *testing.T) {
	c := blackbox.HelperCommand(t, "withoutRuntime")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("ready")
	syscall.Kill(c.Cmd.Process.Pid, syscall.SIGHUP)
	c.ExpectEOFAndWaitForExitCode(fmt.Errorf("exit status 2"))
}
