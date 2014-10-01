// Below is the output from $(binary help -style=godoc ...)

/*
The binary tool facilitates interaction with the veyron binary repository.

Usage:
   binary <command>

The binary commands are:
   delete      Delete binary
   download    Download binary
   upload      Upload binary
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

Binary Delete

Delete connects to the binary repository and deletes the specified binary

Usage:
   binary delete <von>

<von> is the veyron object name of the binary to delete

Binary Download

Download connects to the binary repository, downloads the specified binary, and
writes it to a file.

Usage:
   binary download <von> <filename>

<von> is the veyron object name of the binary to download
<filename> is the name of the file where the binary will be written

Binary Upload

Upload connects to the binary repository and uploads the binary of the specified
file. When successful, it writes the name of the new binary to stdout.

Usage:
   binary upload <von> <filename>

<von> is the veyron object name of the binary to upload
<filename> is the name of the file to upload

Binary Help

Help displays usage descriptions for this command, or usage descriptions for
sub-commands.

Usage:
   binary help [flags] [command ...]

[command ...] is an optional sequence of commands to display detailed usage.
The special-case "help ..." recursively displays help for all commands.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main

import (
	"veyron.io/veyron/veyron/tools/binary/impl"

	"veyron.io/veyron/veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	impl.Root().Main()
}
