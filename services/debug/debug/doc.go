// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command debug supports debugging Vanadium servers.

Usage:
   debug <command>

The debug commands are:
   glob        Returns all matching entries from the namespace.
   vtrace      Returns vtrace traces.
   logs        Accesses log files
   stats       Accesses stats
   pprof       Accesses profiling data
   help        Display help for commands or topics

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
 -metadata=<just specify -metadata to activate>
   Displays metadata for the program and exits.
 -stderrthreshold=2
   logs at or above this threshold go to stderr
 -v=0
   log level for V logs
 -v23.credentials=
   directory to use for storing security credentials
 -v23.i18n-catalogue=
   18n catalogue files to load, comma separated
 -v23.namespace.root=[/(dev.v.io/role/vprod/service/mounttabled)@ns.dev.v.io:8101]
   local namespace root; can be repeated to provided multiple roots
 -v23.proxy=
   object name of proxy service to use to export services across network
   boundaries
 -v23.tcp.address=
   address to listen on
 -v23.tcp.protocol=wsh
   protocol to listen with
 -v23.vtrace.cache-size=1024
   The number of vtrace traces to store in memory.
 -v23.vtrace.collect-regexp=
   Spans and annotations that match this regular expression will trigger trace
   collection.
 -v23.vtrace.dump-on-shutdown=true
   If true, dump all stored traces on runtime shutdown.
 -v23.vtrace.sample-rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -vmodule=
   comma-separated list of pattern=N settings for filename-filtered logging
 -vpath=
   comma-separated list of pattern=N settings for file pathname-filtered logging

Debug glob

Returns all matching entries from the namespace.

Usage:
   debug glob <pattern> ...

<pattern> is a glob pattern to match.

Debug vtrace

Returns matching vtrace traces (or all stored traces if no ids are given).

Usage:
   debug vtrace <name> [id ...]

<name> is the name of a vtrace object. [id] is a vtrace trace id.

Debug logs - Accesses log files

Accesses log files

Usage:
   debug logs <command>

The debug logs commands are:
   read        Reads the content of a log file object.
   size        Returns the size of a log file object.

Debug logs read

Reads the content of a log file object.

Usage:
   debug logs read [flags] <name>

<name> is the name of the log file object.

The debug logs read flags are:
 -f=false
   When true, read will wait for new log entries when it reaches the end of the
   file.
 -n=-1
   The number of log entries to read.
 -o=0
   The position, in bytes, from which to start reading the log file.
 -v=false
   When true, read will be more verbose.

Debug logs size

Returns the size of a log file object.

Usage:
   debug logs size <name>

<name> is the name of the log file object.

Debug stats - Accesses stats

Accesses stats

Usage:
   debug stats <command>

The debug stats commands are:
   read        Returns the value of stats objects.
   watch       Returns a stream of all matching entries and their values as they
               change.

Debug stats read

Returns the value of stats objects.

Usage:
   debug stats read [flags] <name> ...

<name> is the name of a stats object, or a glob pattern to match against stats
object names.

The debug stats read flags are:
 -json=false
   When true, the command will display the raw value of the object in json
   format.
 -raw=false
   When true, the command will display the raw value of the object.
 -type=false
   When true, the type of the values will be displayed.

Debug stats watch

Returns a stream of all matching entries and their values as they change.

Usage:
   debug stats watch [flags] <pattern> ...

<pattern> is a glob pattern to match.

The debug stats watch flags are:
 -raw=false
   When true, the command will display the raw value of the object.
 -type=false
   When true, the type of the values will be displayed.

Debug pprof - Accesses profiling data

Accesses profiling data

Usage:
   debug pprof <command>

The debug pprof commands are:
   run         Runs the pprof tool.
   proxy       Runs an http proxy to a pprof object.

Debug pprof run

Runs the pprof tool.

Usage:
   debug pprof run [flags] <name> <profile> [passthru args] ...

<name> is the name of the pprof object. <profile> the name of the profile to
use.

All the [passthru args] are passed to the pprof tool directly, e.g.

$ debug pprof run a/b/c heap --text $ debug pprof run a/b/c profile -gv

The debug pprof run flags are:
 -pprofcmd=jiri go tool pprof
   The pprof command to use.

Debug pprof proxy

Runs an http proxy to a pprof object.

Usage:
   debug pprof proxy <name>

<name> is the name of the pprof object.

Debug help - Display help for commands or topics

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   debug help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The debug help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=<terminal width>
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
*/
package main
