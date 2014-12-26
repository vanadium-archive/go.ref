package core

import (
	"io"
	"os/exec"

	"v.io/core/veyron/lib/modules"
)

func init() {
	modules.RegisterChild(ExecCommand, "", execCommand)
}

func execCommand(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	cmd := exec.Command(args[1], args[2:]...)
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, key+"="+value)
	}

	cmd.Env = envSlice
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
