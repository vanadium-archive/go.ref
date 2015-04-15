// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command vrpc sends and receives Vanadium remote procedure calls.  It is used as
a generic client to interact with any Vanadium server.

Usage:
   vrpc <command>

The vrpc commands are:
   signature   Describe the interfaces of a Vanadium server
   call        Call a method of a Vanadium server
   identify    Reveal blessings presented by a Vanadium server
   help        Display help for commands or topics
Run "vrpc help [command]" for command usage.

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
   comma-separated list of pattern=N settings for file-filtered logging

Vrpc Signature

Signature connects to the Vanadium server identified by <server>.

If no [method] is provided, returns all interfaces implemented by the server.

If a [method] is provided, returns the signature of just that method.

Usage:
   vrpc signature [flags] <server> [method]

<server> identifies a Vanadium server.  It can either be the object address of
the server, or an object name that will be resolved to an end-point.

[method] is the optional server method name.

The vrpc signature flags are:
 -insecure=false
   If true, skip server authentication. This means that the client will reveal
   its blessings to servers that it may not recognize.
 -show-reserved=false
   if true, also show the signatures of reserved methods

Vrpc Call

Call connects to the Vanadium server identified by <server> and calls the
<method> with the given positional [args...], returning results on stdout.

TODO(toddw): stdin is read for streaming arguments sent to the server.  An EOF
on stdin (e.g. via ^D) causes the send stream to be closed.

Regardless of whether the call is streaming, the main goroutine blocks for
streaming and positional results received from the server.

All input arguments (both positional and streaming) are specified as VDL
expressions, with commas separating multiple expressions.  Positional arguments
may also be specified as separate command-line arguments.  Streaming arguments
may also be specified as separate newline-terminated expressions.

The method signature is always retrieved from the server as a first step.  This
makes it easier to input complex typed arguments, since the top-level type for
each argument is implicit and doesn't need to be specified.

Usage:
   vrpc call <server> <method> [args...]

<server> identifies a Vanadium server.  It can either be the object address of
the server, or an object name that will be resolved to an end-point.

<method> is the server method to call.

[args...] are the positional input arguments, specified as VDL expressions.

Vrpc Identify

Identify connects to the Vanadium server identified by <server> and dumps out
the blessings presented by that server (and the subset of those that are
considered valid by the principal running this tool) to standard output.

Usage:
   vrpc identify [flags] <server>

<server> identifies a Vanadium server.  It can either be the object address of
the server, or an object name that will be resolved to an end-point.

The vrpc identify flags are:
 -insecure=false
   If true, skip server authentication. This means that the client will reveal
   its blessings to servers that it may not recognize.

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
 -style=default
   The formatting style for help output, either "default" or "godoc".
*/
package main
