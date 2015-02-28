// test_child is a helper package for testing two binaries under the same agent.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"v.io/core/veyron/lib/flags/consts"
)

var (
	pingpong, rootMT string
)

func init() {
	flag.StringVar(&rootMT, "rootmt", "", "name for the root mounttable")
	flag.StringVar(&pingpong, "pingpong", "", "path name to pingpong binary")
}

func main() {
	flag.Parse()
	if os.Getenv(consts.VeyronCredentials) != "" {
		panic("VEYRON_CREDENTIALS is set - identity preserved.")
	}
	if len(rootMT) == 0 {
		panic("no rootmt parameter")
	}
	fmt.Fprintf(os.Stderr, "NS ROOT: %v\n\n", os.Getenv(consts.NamespaceRootPrefix))

	if len(pingpong) == 0 {
		panic("no pingpong binary supplied")
	}
	ns := consts.NamespaceRootPrefix + "=" + rootMT
	server := exec.Command(pingpong, "--server")
	server.Env = append(server.Env, ns)
	client := exec.Command(pingpong)
	client.Env = append(client.Env, ns)

	err := server.Start()
	if err != nil {
		panic(fmt.Sprintf("failed to start server: %s", err))
	}
	defer func() {
		syscall.Kill(server.Process.Pid, syscall.SIGTERM)
		if err = server.Wait(); err != nil {
			panic(fmt.Sprintf("failed to stop server: %s", err))
		}

	}()
	fmt.Println("running client")
	output, err := client.Output()
	if err != nil {
		panic(fmt.Sprintf("failed to run client: %s: output: %q", err, output))
	}
	fmt.Println("client output")
	fmt.Println(string(output))
}
