package modules_test

import (
	"fmt"
	"io"
	"os"

	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
)

func init() {
	modules.RegisterChild("echo", "<args>...", echo)
}

func echo(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	for i, a := range args {
		fmt.Fprintf(stdout, "%d: %s\n", i, a)
	}
	return nil
}

func ExampleDispatch() {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	if modules.IsModulesChildProcess() {
		// Child process. Dispatch will invoke the 'echo' command
		if err := modules.Dispatch(); err != nil {
			panic(fmt.Sprintf("unexpected error: %s", err))
		}
		return
	}
	// Parent process.
	sh, _ := modules.NewShell(ctx, nil, false, nil)
	defer sh.Cleanup(nil, nil)
	h, _ := sh.Start("echo", nil, "a", "b")
	h.Shutdown(os.Stdout, os.Stderr)
	// Output:
	// 0: a
	// 1: b
}

func ExampleDispatchAndExit() {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	// DispatchAndExit will call os.Exit(0) when executed within the child.
	modules.DispatchAndExit()
	sh, _ := modules.NewShell(ctx, nil, false, nil)
	defer sh.Cleanup(nil, nil)
	h, _ := sh.Start("echo", nil, "c", "d")
	h.Shutdown(os.Stdout, os.Stderr)
	// Output:
	// 0: c
	// 1: d
}
