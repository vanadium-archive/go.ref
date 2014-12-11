// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The mounttable tool facilitates interaction with a Veyron mount table.

Usage:
   mounttable <command>

The mounttable commands are:
   glob        returns all matching entries in the mount table
   mount       Mounts a server <name> onto a mount table
   unmount     removes server <name> from the mount table
   resolvestep takes the next step in resolving a name.
   help        Display help for commands or topics
Run "mounttable help [command]" for command usage.

The global flags are:
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
 -veyron.namespace.root=[/ns.dev.v.io:8101]
   local namespace root; can be repeated to provided multiple roots
 -veyron.vtrace.cache_size=1024
   The number of vtrace traces to store in memory.
 -veyron.vtrace.dump_on_shutdown=false
   If true, dump all stored traces on runtime shutdown.
 -veyron.vtrace.sample_rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -vmodule=
   comma-separated list of pattern=N settings for file-filtered logging

Mounttable Glob

returns all matching entries in the mount table

Usage:
   mounttable glob [<mount name>] <pattern>

<mount name> is a mount name on a mount table.  Defaults to namespace root.
<pattern> is a glob pattern that is matched against all the entries below the
specified mount name.

Mounttable Mount

Mounts a server <name> onto a mount table

Usage:
   mounttable mount <mount name> <name> <ttl>

<mount name> is a mount name on a mount table. <name> is the rooted object name
of the server. <ttl> is the TTL of the new entry. It is a decimal number
followed by a unit suffix (s, m, h). A value of 0s represents an infinite
duration.

Mounttable Unmount

removes server <name> from the mount table

Usage:
   mounttable unmount <mount name> <name>

<mount name> is a mount name on a mount table. <name> is the rooted object name
of the server.

Mounttable Resolvestep

takes the next step in resolving a name.

Usage:
   mounttable resolvestep <mount name>

<mount name> is a mount name on a mount table.

Mounttable Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   mounttable help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The mounttable help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
