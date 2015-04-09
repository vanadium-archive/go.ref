// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command build sends commands to a Vanadium build server.

Usage:
   build <command>

The build commands are:
   build       Build vanadium Go packages
   help        Display help for commands or topics
Run "build help [command]" for command usage.

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

Build Build

Build vanadium Go packages using a remote build server. The command collects all
source code files that are not part of the Go standard library that the target
packages depend on, sends them to a build server, and receives the built
binaries.

Usage:
   build build [flags] <name> <packages>

<name> is a vanadium object name of a build server <packages> is a list of
packages to build, specified as arguments for each command. The format is
similar to the go tool.  In its simplest form each package is an import path;
e.g. "v.io/x/ref/services/build/build". A package that ends with "..." does a
wildcard match against all packages with that prefix.

The build build flags are:
 -arch=<runtime.GOARCH>
   Target architecture.  The default is the value of runtime.GOARCH.
 -os=<runtime.GOOS>
   Target operating system.  The default is the value of runtime.GOOS.

Build Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   build help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The build help flags are:
 -style=default
   The formatting style for help output, either "default" or "godoc".
*/
package main
