package impl

import (
	"flag"
)

func Run(environ []string) error {
	var work WorkParameters
	if err := work.ProcessArguments(flag.CommandLine, environ); err != nil {
		return err
	}

	if work.remove {
		return work.Remove()
	}

	if err := work.Chown(); err != nil {
		return err
	}

	return work.Exec()
}
