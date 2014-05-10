package rt_test

import (
	"fmt"
	"syscall"
	"testing"

	"veyron/lib/testutil/blackbox"
	"veyron/runtimes/google/rt"
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
	rt.Init()
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
