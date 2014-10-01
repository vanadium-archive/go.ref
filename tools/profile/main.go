// Below is the output from $(profile help -style=godoc ...)

/*
The profile tool facilitates interaction with the veyron profile repository.

Usage:
   profile <command>

The profile commands are:
   label       Shows a human-readable profile key for the profile.
   description Shows a human-readable profile description for the profile.
   spec        Shows the specification of the profile.
   put         Sets a placeholder specification for the profile.
   remove      removes the profile specification for the profile.
   help        Display help for commands

The global flags are:
   -alsologtostderr=true: log to standard error as well as files
   -log_backtrace_at=:0: when logging hits line file:N, emit a stack trace
   -log_dir=: if non-empty, write log files to this directory
   -logtostderr=false: log to standard error instead of files
   -max_stack_buf_size=4292608: max size in bytes of the buffer to use for logging stack traces
   -stderrthreshold=2: logs at or above this threshold go to stderr
   -v=0: log level for V logs
   -vmodule=: comma-separated list of pattern=N settings for file-filtered logging
   -vv=0: log level for V logs

Profile Label

Shows a human-readable profile key for the profile.

Usage:
   profile label <profile>

<profile> is the full name of the profile.

Profile Description

Shows a human-readable profile description for the profile.

Usage:
   profile description <profile>

<profile> is the full name of the profile.

Profile Spec

Shows the specification of the profile.

Usage:
   profile spec <profile>

<profile> is the full name of the profile.

Profile Put

Sets a placeholder specification for the profile.

Usage:
   profile put <profile>

<profile> is the full name of the profile.

Profile Remove

removes the profile specification for the profile.

Usage:
   profile remove <profile>

<profile> is the full name of the profile.

Profile Help

Help displays usage descriptions for this command, or usage descriptions for
sub-commands.

Usage:
   profile help [flags] [command ...]

[command ...] is an optional sequence of commands to display detailed usage.
The special-case "help ..." recursively displays help for all commands.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main

import (
	"veyron.io/veyron/veyron/tools/profile/impl"

	"veyron.io/veyron/veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	impl.Root().Main()
}
