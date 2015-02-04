// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The binary tool facilitates interaction with the veyron binary repository.

Usage:
   binary <command>

The binary commands are:
   delete      Delete a binary
   download    Download a binary
   upload      Upload a binary
   url         Fetch a download URL
   help        Display help for commands or topics
Run "binary help [command]" for command usage.

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
 -vanadium.i18n_catalogue=
   18n catalogue files to load, comma separated
 -veyron.credentials=
   directory to use for storing security credentials
 -veyron.namespace.root=[/ns.dev.v.io:8101]
   local namespace root; can be repeated to provided multiple roots
 -veyron.proxy=
   object name of proxy service to use to export services across network
   boundaries
 -veyron.tcp.address=
   address to listen on
 -veyron.tcp.protocol=tcp
   protocol to listen with
 -veyron.vtrace.cache_size=1024
   The number of vtrace traces to store in memory.
 -veyron.vtrace.collect_regexp=
   Spans and annotations that match this regular expression will trigger trace
   collection.
 -veyron.vtrace.dump_on_shutdown=true
   If true, dump all stored traces on runtime shutdown.
 -veyron.vtrace.sample_rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -vmodule=
   comma-separated list of pattern=N settings for file-filtered logging

Binary Delete

Delete connects to the binary repository and deletes the specified binary

Usage:
   binary delete <von>

<von> is the veyron object name of the binary to delete

Binary Download

Download connects to the binary repository, downloads the specified binary, and
writes it to a file.

Usage:
   binary download <von> <filename>

<von> is the veyron object name of the binary to download <filename> is the name
of the file where the binary will be written

Binary Upload

Upload connects to the binary repository and uploads the binary of the specified
file. When successful, it writes the name of the new binary to stdout.

Usage:
   binary upload <von> <filename>

<von> is the veyron object name of the binary to upload <filename> is the name
of the file to upload

Binary Url

Connect to the binary repository and fetch the download URL for the given veyron
object name.

Usage:
   binary url <von>

<von> is the veyron object name of the binary repository

Binary Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   binary help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The binary help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
