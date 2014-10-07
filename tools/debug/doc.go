// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command-line tool for interacting with the debug server.

Usage:
   debug <command>

The debug commands are:
   glob        Returns all matching entries from the namespace
   logs        Accesses log files
   stats       Accesses stats
   pprof       Accesses profiling data
   help        Display help for commands or topics
Run "debug help [command]" for command usage.

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

Debug Glob

Returns all matching entries from the namespace.

Usage:
   debug glob <pattern> ...

<pattern> is a glob pattern to match.

Debug Logs

Accesses log files

Usage:
   debug logs <command>

The logs commands are:
   read        Reads the content of a log file object.
   size        Returns the size of the a log file object

Debug Logs Read

Reads the content of a log file object.

Usage:
   debug logs read [flags] <name>

<name> is the name of the log file object.

The read flags are:
   -f=false: When true, read will wait for new log entries when it reaches the end of the file.
   -n=-1: The number of log entries to read.
   -o=0: The position, in bytes, from which to start reading the log file.
   -v=false: When true, read will be more verbose.

Debug Logs Size

Returns the size of the a log file object.

Usage:
   debug logs size <name>

<name> is the name of the log file object.

Debug Stats

Accesses stats

Usage:
   debug stats <command>

The stats commands are:
   value       Returns the value of the a stats object
   watchglob   Returns a stream of all matching entries and their values

Debug Stats Value

Returns the value of the a stats object.

Usage:
   debug stats value [flags] <name>

<name> is the name of the stats object.

The value flags are:
   -raw=false: When true, the command will display the raw value of the object.
   -type=false: When true, the type of the values will be displayed.

Debug Stats Watchglob

Returns a stream of all matching entries and their values

Usage:
   debug stats watchglob [flags] <pattern> ...

<pattern> is a glob pattern to match.

The watchglob flags are:
   -raw=false: When true, the command will display the raw value of the object.
   -type=false: When true, the type of the values will be displayed.

Debug Pprof

Accesses profiling data

Usage:
   debug pprof <command>

The pprof commands are:
   run         Runs the pprof tool
   proxy       Runs an http proxy to a pprof object

Debug Pprof Run

Runs the pprof tool

Usage:
   debug pprof run [flags] <name> <profile> [passthru args] ...

<name> is the name of the pprof object.
<profile> the name of the profile to use.

All the [passthru args] are passed to the pprof tool directly, e.g.

$ debug pprof run a/b/c heap --text --lines
$ debug pprof run a/b/c profile -gv

The run flags are:
   -pprofcmd=veyron go tool pprof: The pprof command to use.

Debug Pprof Proxy

Runs an http proxy to a pprof object

Usage:
   debug pprof proxy <name>

<name> is the name of the pprof object.

Debug Help

Help with no args displays the usage of the parent command.
Help with args displays the usage of the specified sub-command or help topic.
"help ..." recursively displays help for all commands and topics.

Usage:
   debug help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main
