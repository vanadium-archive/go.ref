// Below is the output from $(namespace help -style=godoc ...)

/*
The namespace tool facilitates interaction with the Veyron namespace.

The namespace roots are set from environment variables that have a name
starting with NAMESPACE_ROOT, e.g. NAMESPACE_ROOT, NAMESPACE_ROOT_2,
NAMESPACE_ROOT_GOOGLE, etc.

Usage:
   namespace <command>

The namespace commands are:
   glob        Returns all matching entries from the namespace
   mount       Adds a server to the namespace
   unmount     Removes a server from the namespace
   resolve     Translates a object name to its object address(es)
   resolvetomt Finds the address of the mounttable that holds an object name
   unresolve   Returns the rooted object names for the given object name
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

Namespace Glob

Returns all matching entries from the namespace.

Usage:
   namespace glob <pattern>

<pattern> is a glob pattern that is matched against all the names below the
specified mount name.

Namespace Mount

Adds server <server> to the namespace with name <name>.

Usage:
   namespace mount <name> <server> <ttl>

<name> is the name to add to the namespace.
<server> is the object address of the server to add.
<ttl> is the TTL of the new entry. It is a decimal number followed by a unit
suffix (s, m, h). A value of 0s represents an infinite duration.

Namespace Unmount

Removes server <server> with name <name> from the namespace.

Usage:
   namespace unmount <name> <server>

<name> is the name to remove from the namespace.
<server> is the object address of the server to remove.

Namespace Resolve

Translates a object name to its object address(es).

Usage:
   namespace resolve <name>

<name> is the name to resolve.

Namespace Resolvetomt

Finds the address of the mounttable that holds an object name.

Usage:
   namespace resolvetomt <name>

<name> is the name to resolve.

Namespace Unresolve

Returns the rooted object names for the given object name.

Usage:
   namespace unresolve <name>

<name> is the object name to unresolve.

Namespace Help

Help displays usage descriptions for this command, or usage descriptions for
sub-commands.

Usage:
   namespace help [flags] [command ...]

[command ...] is an optional sequence of commands to display detailed usage.
The special-case "help ..." recursively displays help for all commands.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main

import (
	"veyron.io/veyron/veyron/tools/namespace/impl"
	"veyron.io/veyron/veyron2/rt"
)

func main() {
	defer rt.Init().Cleanup()
	impl.Root().Main()
}
