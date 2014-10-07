// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The build tool tool facilitates interaction with the veyron build server.

Usage:
   build <command>

The build commands are:
   build       Build veyron Go packages
   help        Display help for commands or topics
Run "build help [command]" for command usage.

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

Build Build

Build veyron Go packages using a remote build server. The command
collects all source code files that are not part of the Go standard
library that the target packages depend on, sends them to a build
server, and receives the built binaries.

Usage:
   build build [flags] <name> <packages>

<name> is a veyron object name of a build server
<packages> is a list of packages to build, specified as arguments for
each command. The format is similar to the go tool.  In its simplest
form each package is an import path; e.g. "veyron/tools/build". A
package that ends with "..." does a wildcard match against all
packages with that prefix.

The build flags are:
   -arch=amd64: Target architecture.
   -os=linux: Target operating system.

Build Help

Help with no args displays the usage of the parent command.
Help with args displays the usage of the specified sub-command or help topic.
"help ..." recursively displays help for all commands and topics.

Usage:
   build help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main
