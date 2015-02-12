package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Helper script for testing vrun.
func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "usage: %s <vrun_path> <pingpong_path> <principal_path>\n", os.Args[0])
		os.Exit(1)
	}

	vrunPath := os.Args[1]
	pingpongPath := os.Args[2]
	principalPath := os.Args[3]

	if output, err := exec.Command(principalPath, "dump").Output(); err != nil {
		fmt.Fprintf(os.Stderr, "could not run %s dump\n", principalPath)
		os.Exit(1)
	} else {
		if want := "Default blessings: agent_principal"; !strings.Contains(string(output), want) {
			fmt.Fprintf(os.Stderr, "expected output to contain %s, but did not. Output was:\n%s\n")
			os.Exit(1)
		}
	}

	if output, err := exec.Command(vrunPath, principalPath, "dump").Output(); err != nil {
		fmt.Fprintf(os.Stderr, "could not run %s %s dump\n", vrunPath, principalPath)
		os.Exit(1)
	} else {
		if want := "Default blessings: agent_principal/principal"; !strings.Contains(string(output), want) {
			fmt.Fprintf(os.Stderr, "expected output to contain %s, but did not. Output was:\n%s\n")
			os.Exit(1)
		}
	}

	if output, err := exec.Command(vrunPath, "--name=foo", principalPath, "dump").Output(); err != nil {
		fmt.Fprintf(os.Stderr, "could not run %s %s dump\n", vrunPath, principalPath)
		os.Exit(1)
	} else {
		if want := "Default blessings: agent_principal/foo"; !strings.Contains(string(output), want) {
			fmt.Fprintf(os.Stderr, "expected output to contain %s, but did not. Output was:\n%s\n")
			os.Exit(1)
		}
	}

	server, err := os.StartProcess(vrunPath, []string{filepath.Base(vrunPath), pingpongPath, "--server"}, &os.ProcAttr{})
	defer func() {
		if server != nil {
			server.Kill()
		}
	}()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not start server: %v\n", err)
		os.Exit(1)
	}

	if output, err := exec.Command(vrunPath, pingpongPath).Output(); err != nil {
		fmt.Fprintf(os.Stderr, "could not start client: %v\n", err)
		os.Exit(1)
	} else {
		fmt.Fprintf(os.Stdout, "Received output: %s\n", output)
	}
}
