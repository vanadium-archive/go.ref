package core

import (
	"io"
	"syscall"

	"v.io/core/veyron/lib/modules"
)

func init() {
	modules.RegisterChild(ExecCommand, "", execCommand)
}

func execCommand(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, key+"="+value)
	}
	return syscall.Exec(args[1], args[1:], envSlice)
}
