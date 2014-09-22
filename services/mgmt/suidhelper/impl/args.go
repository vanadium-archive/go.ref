package impl

import (
	"flag"
	"fmt"
	"os/user"
)

type WorkParameters struct {
	uid       string
	gid       string
	workspace string
	stderrLog string
	stdoutLog string
	argv0     string
	argv      []string
	envv      []string
}

var flagUsername, flagWorkspace, flagStdoutLog, flagStderrLog, flagRun *string

func init() {
	// Add flags to global set.
	setupFlags(flag.CommandLine)
}

func setupFlags(fs *flag.FlagSet) {
	flagUsername = fs.String("username", "", "The UNIX user name used for the other functions of this tool.")
	flagWorkspace = fs.String("workspace", "", "Path to the application's workspace directory.")
	flagStdoutLog = fs.String("stdoutlog", "", "Path to the stdout log file.")
	flagStderrLog = fs.String("stderrlog", "", "Path to the stdin log file.")
	flagRun = fs.String("run", "", "Path to the application to exec.")
}

// ParseArguments populates the WorkParameter object from the provided args
// and env strings.
func (wp *WorkParameters) ProcessArguments(fs *flag.FlagSet, env []string) error {
	username := *flagUsername
	if username == "" {
		return fmt.Errorf("--username missing")
	}

	usr, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("--username %s: unknown user", username)
	}

	wp.uid = usr.Uid
	wp.gid = usr.Gid
	wp.workspace = *flagWorkspace
	wp.argv0 = *flagRun
	wp.stdoutLog = *flagStdoutLog
	wp.stderrLog = *flagStderrLog
	wp.argv = fs.Args()
	wp.envv = env

	return nil
}
