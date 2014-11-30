// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The vrpc tool facilitates interaction with Veyron RPC servers. In particular, it
can be used to 1) find out what API a Veyron RPC server exports and 2) send
requests to a Veyron RPC server.

Usage:
   vrpc <command>

The vrpc commands are:
   describe    Describe the API of an Veyron RPC server
   invoke      Invoke a method of an Veyron RPC server
   help        Display help for commands or topics
Run "vrpc help [command]" for command usage.

The global flags are:
 -acl=
   acl is an optional JSON-encoded security.ACL that is used to construct a
   security.Authorizer. Example: {"In":{"veyron/alice/...":"RW"}} is a
   JSON-encoded ACL that allows all delegates of "veyron/alice" to access all
   methods with ReadLabel or WriteLabel. If this flag is provided then the
   \"--acl_file\" must be absent.
 -acl_file=
   acl_file is an optional path to a file containing a JSON-encoded security.ACL
   that is used to construct a security.Authorizer. If this flag is provided
   then the "--acl_file" flag must be absent.
 -alsologtostderr=true
   log to standard error as well as files
 -log_backtrace_at=:0
   when logging hits line file:N, emit a stack trace
 -log_dir=
   if non-empty, write log files to this directory
 -logtostderr=false
   log to standard error instead of files
 -max_stack_buf_size=4292608
   max size in bytes of the buffer to use for logging stack traces
 -stderrthreshold=2
   logs at or above this threshold go to stderr
 -v=0
   log level for V logs
 -veyron.credentials=
   directory to use for storing security credentials
 -veyron.namespace.root=[/proxy.envyor.com:8101]
   local namespace root; can be repeated to provided multiple roots
 -veyron.proxy=
   object name of proxy service to use to export services across network
   boundaries
 -veyron.tcp.address=:0
   address to listen on
 -veyron.tcp.protocol=tcp
   protocol to listen with
 -veyron.vtrace.cache_size=1024
   The number of vtrace traces to store in memory.
 -veyron.vtrace.dump_on_shutdown=false
   If true, dump all stored traces on runtime shutdown.
 -veyron.vtrace.sample_rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -vmodule=
   comma-separated list of pattern=N settings for file-filtered logging

Vrpc Describe

Describe connects to the Veyron RPC server identified by <server>, finds out
what its API is, and outputs a succint summary of this API to the standard
output.

Usage:
   vrpc describe <server>

<server> identifies the Veyron RPC server. It can either be the object address
of the server or an Object name in which case the vrpc will use Veyron's name
resolution to match this name to an end-point.

Vrpc Invoke

Invoke connects to the Veyron RPC server identified by <server>, invokes the
method identified by <method>, supplying the arguments identified by <args>, and
outputs the results of the invocation to the standard output.

Usage:
   vrpc invoke <server> <method> <args>

<server> identifies the Veyron RPC server. It can either be the object address
of the server or an Object name in which case the vrpc will use Veyron's name
resolution to match this name to an end-point.

<method> identifies the name of the method to be invoked.

<args> identifies the arguments of the method to be invoked. It should be a list
of values in a VOM JSON format that can be reflected to the correct type using
Go's reflection.

Vrpc Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   vrpc help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The vrpc help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
