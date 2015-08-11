// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command agentd runs the security agent daemon, which holds a private key in
memory and makes it available to a subprocess.

Loads the private key specified in privatekey.pem in V23_CREDENTIALS into
memory, then starts the specified command with access to the private key via the
agent protocol instead of directly reading from disk.

Usage:
   agentd [flags] command [command_args...]

The command is started as a subprocess with the given [command_args...].

The agentd flags are:
 -additional-principals=
   If non-empty, allow for the creation of new principals and save them in this
   directory.
 -new-principal-blessing-name=
   If creating a new principal (--v23.credentials does not exist), then have it
   blessed with this name.
 -no-passphrase=false
   If true, user will not be prompted for principal encryption passphrase.
 -restart-exit-code=
   If non-empty, will restart the command when it exits, provided that the
   command's exit code matches the value of this flag.  The value must be an
   integer, or an integer preceded by '!' (in which case all exit codes except
   the flag will trigger a restart).

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
*/
package main
