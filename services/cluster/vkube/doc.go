// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Manages Vanadium applications on kubernetes

Usage:
   vkube [flags] <command>

The vkube commands are:
   start               Starts an application.
   update              Updates an application.
   stop                Stops an application.
   start-cluster-agent Starts the cluster agent.
   stop-cluster-agent  Stops the cluster agent.
   claim-cluster-agent Claims the cluster agent.
   build-docker-images Builds the docker images for the cluster and pod agents.
   ctl                 Runs kubectl on the cluster defined in vkube.cfg.
   help                Display help for commands or topics

The vkube flags are:
 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

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
 -time=false
   Dump timing information to stderr before exiting the program.
 -v=0
   log level for V logs
 -v23.credentials=
   directory to use for storing security credentials
 -v23.i18n-catalogue=
   18n catalogue files to load, comma separated
 -v23.namespace.root=[/(dev.v.io:role:vprod:service:mounttabled)@ns.dev.v.io:8101]
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
 -v23.vtrace.v=0
   The verbosity level of the log messages to be captured in traces
 -vmodule=
   comma-separated list of globpattern=N settings for filename-filtered logging
   (without the .go suffix).  E.g. foo/bar/baz.go is matched by patterns baz or
   *az or b* but not by bar/baz or baz.go or az or b.*
 -vpath=
   comma-separated list of regexppattern=N settings for file pathname-filtered
   logging (without the .go suffix).  E.g. foo/bar/baz.go is matched by patterns
   foo/bar/baz or fo.*az or oo/ba or b.z but not by foo/bar/baz.go or fo*az

Vkube start

Starts an application.

Usage:
   vkube start [flags] <extension>

<extension> The blessing name extension to give to the application.

The vkube start flags are:
 -f=
   Filename to use to create the kubernetes resource.
 -wait=false
   Wait for at least one replica to be ready.

 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube update

Updates an application to a new version with a rolling update, preserving the
existing blessings.

Usage:
   vkube update [flags]

The vkube update flags are:
 -f=
   Filename to use to update the kubernetes resource.
 -wait=false
   Wait for at least one replica to be ready after the update.

 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube stop

Stops an application.

Usage:
   vkube stop [flags]

The vkube stop flags are:
 -f=
   Filename to use to stop the kubernetes resource.

 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube start-cluster-agent

Starts the cluster agent.

Usage:
   vkube start-cluster-agent [flags]

The vkube start-cluster-agent flags are:
 -wait=false
   Wait for the cluster agent to be ready.

 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube stop-cluster-agent

Stops the cluster agent.

Usage:
   vkube stop-cluster-agent [flags]

The vkube stop-cluster-agent flags are:
 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube claim-cluster-agent

Claims the cluster agent.

Usage:
   vkube claim-cluster-agent [flags]

The vkube claim-cluster-agent flags are:
 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube build-docker-images

Builds the docker images for the cluster and pod agents.

Usage:
   vkube build-docker-images [flags]

The vkube build-docker-images flags are:
 -tag=
   The tag to add to the docker images. If empty, the current timestamp is used.
 -v=false
   When true, the output is more verbose.

 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube ctl

Runs kubectl on the cluster defined in vkube.cfg.

Usage:
   vkube ctl [flags] -- <kubectl args>

<kubectl args> are passed directly to the kubectl command.

The vkube ctl flags are:
 -config=vkube.cfg
   The 'vkube.cfg' file to use.
 -gcloud=gcloud
   The 'gcloud' binary to use.
 -kubectl=kubectl
   The 'kubectl' binary to use.

Vkube help - Display help for commands or topics

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   vkube help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The vkube help flags are:
 -style=compact
   The formatting style for help output:
      compact   - Good for compact cmdline output.
      full      - Good for cmdline output, shows all global flags.
      godoc     - Good for godoc processing.
      shortonly - Only output short description.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=<terminal width>
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
*/
package main
