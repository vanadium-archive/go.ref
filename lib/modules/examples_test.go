package modules_test

import (
	"fmt"
	"io"
	"os"

	"v.io/core/veyron/lib/modules"
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
	if modules.IsModulesProcess() {
		// Child process. Dispatch will invoke the 'echo' command
		if err := modules.Dispatch(); err != nil {
			panic(fmt.Sprintf("unexpected error: %s", err))
		}
		return
	}
	// Parent process.
	sh, _ := modules.NewShell(nil)
	defer sh.Cleanup(nil, nil)
	h, _ := sh.Start("echo", nil, "a", "b")
	h.Shutdown(os.Stdout, os.Stderr)
	// Output:
	// 0: echo
	// 1: a
	// 2: b
}

func ExampleDispatchAndExit() {
	// DispatchAndExit will call os.Exit(0) when executed within the child.
	modules.DispatchAndExit()
	sh, _ := modules.NewShell(nil)
	defer sh.Cleanup(nil, nil)
	h, _ := sh.Start("echo", nil, "c", "d")
	h.Shutdown(os.Stdout, os.Stderr)
	// Output:
	// 0: echo
	// 1: c
	// 2: d
}
