// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

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
   help        Display help for commands or topics
Run "profile help [command]" for command usage.

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
 -veyron.namespace.root=[/proxy.envyor.com:8101]
   local namespace root; can be repeated to provided multiple roots
 -veyron.vtrace.cache_size=1024
   The number of vtrace traces to store in memory.
 -veyron.vtrace.dump_on_shutdown=false
   If true, dump all stored traces on runtime shutdown.
 -veyron.vtrace.sample_rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -vmodule=
   comma-separated list of pattern=N settings for file-filtered logging

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

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   profile help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The profile help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
