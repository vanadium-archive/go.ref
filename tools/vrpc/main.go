// Below is the output from $(vrpc help -style=godoc ...)

/*
The vrpc tool facilitates interaction with Veyron RPC servers. In particular,
it can be used to 1) find out what API a Veyron RPC server exports and
2) send requests to a Veyron RPC server.

Usage:
   vrpc <command>

The vrpc commands are:
   describe    Describe the API of an Veyron RPC server
   invoke      Invoke a method of an Veyron RPC server
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

Vrpc Describe

Describe connects to the Veyron RPC server identified by <server>, finds out what
its API is, and outputs a succint summary of this API to the standard output.

Usage:
   vrpc describe <server>

<server> identifies the Veyron RPC server. It can either be the object address of
the server or an Object name in which case the vrpc will use Veyron's name
resolution to match this name to an end-point.

Vrpc Invoke

Invoke connects to the Veyron RPC server identified by <server>, invokes the method
identified by <method>, supplying the arguments identified by <args>, and outputs
the results of the invocation to the standard output.

Usage:
   vrpc invoke <server> <method> <args>

<server> identifies the Veyron RPC server. It can either be the object address of
the server or an Object name in which case the vrpc will use Veyron's name
resolution to match this name to an end-point.

<method> identifies the name of the method to be invoked.

<args> identifies the arguments of the method to be invoked. It should be a list
of values in a VOM JSON format that can be reflected to the correct type
using Go's reflection.

Vrpc Help

Help displays usage descriptions for this command, or usage descriptions for
sub-commands.

Usage:
   vrpc help [flags] [command ...]

[command ...] is an optional sequence of commands to display detailed usage.
The special-case "help ..." recursively displays help for all commands.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main

import (
	"veyron.io/veyron/veyron/tools/vrpc/impl"

	"veyron.io/veyron/veyron2/rt"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()
	impl.Root().Main()
}
