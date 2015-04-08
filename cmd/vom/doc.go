// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command vom helps debug the Vanadium Object Marshaling wire protocol.

Usage:
   vom <command>

The vom commands are:
   decode      Decode data encoded in the vom format
   dump        Dump data encoded in the vom format into formatted output
   help        Display help for commands or topics
Run "vom help [command]" for command usage.

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
 -v23.namespace.root=[/ns.dev.v.io:8101]
   local namespace root; can be repeated to provided multiple roots
 -v23.permissions.file=map[]
   specify an acl file as <name>:<aclfile>
 -v23.permissions.literal=
   explicitly specify the runtime acl as a JSON-encoded access.Permissions.
   Overrides all --v23.permissions.file flags.
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

Vom Decode

Decode decodes data encoded in the vom format.  If no arguments are provided,
decode reads the data from stdin, otherwise the argument is the data.

By default the data is assumed to be represented in hex, with all whitespace
anywhere in the data ignored.  Use the -data flag to specify other data
representations.

Usage:
   vom decode [flags] [data]

[data] is the data to decode; if not specified, reads from stdin

The vom decode flags are:
 -data=Hex
   Data representation, one of [Hex Binary]

Vom Dump

Dump dumps data encoded in the vom format, generating formatted output
describing each portion of the encoding.  If no arguments are provided, dump
reads the data from stdin, otherwise the argument is the data.

By default the data is assumed to be represented in hex, with all whitespace
anywhere in the data ignored.  Use the -data flag to specify other data
representations.

Calling "vom dump" with no flags and no arguments combines the default stdin
mode with the default hex mode.  This default mode is special; certain non-hex
characters may be input to represent commands:
  . (period)    Calls Dumper.Status to get the current decoding status.
  ; (semicolon) Calls Dumper.Flush to flush output and start a new message.

This lets you cut-and-paste hex strings into your terminal, and use the commands
to trigger status or flush calls; i.e. a rudimentary debugging UI.

See v.io/v23/vom.Dumper for details on the dump output.

Usage:
   vom dump [flags] [data]

[data] is the data to dump; if not specified, reads from stdin

The vom dump flags are:
 -data=Hex
   Data representation, one of [Hex Binary]

Vom Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   vom help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The vom help flags are:
 -style=default
   The formatting style for help output, either "default" or "godoc".
*/
package main
