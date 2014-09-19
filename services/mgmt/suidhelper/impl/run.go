package impl

import (
	"flag"
)

func Run(environ []string) error {
	var work WorkParameters
	if err := work.ProcessArguments(flag.CommandLine, environ); err != nil {
		return err
	}

	// 1. For each chown directory, chown.
	if err := work.Chown(); err != nil {
		return err
	}

	// 2. Run the command if it exists.
	return work.Exec()
}
