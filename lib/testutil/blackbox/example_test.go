package blackbox_test

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"veyron.io/veyron/veyron/lib/testutil/blackbox"
)

func init() {
	blackbox.CommandTable["print"] = print
	blackbox.CommandTable["echo"] = echo
}

func print(args []string) {
	for _, v := range args {
		fmt.Printf("%s\n", v)
	}
	blackbox.WaitForEOFOnStdin()
	fmt.Printf("done\n")
}

func echo(args []string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("error reading stdin: %s", err)
	} else {
		fmt.Printf("done\n")
	}
}

func ExampleEcho() {
	// Normally t is provided by testing and this example can't
	// possible work outside of that environment.
	t := &testing.T{}
	c := blackbox.HelperCommand(t, "print", "a", "b", "c")
	defer c.Cleanup()
	c.Cmd.Start()
	c.Expect("a")
	c.Expect("b")
	c.Expect("c")
	c.CloseStdin()
	c.Expect("done")
	c.ExpectEOFAndWait()
	if !t.Failed() {
		fmt.Printf("ok\n")
	}
}
