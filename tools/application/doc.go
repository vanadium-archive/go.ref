// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The application tool facilitates interaction with the veyron application
repository.

Usage:
   application <command>

The application commands are:
   match       Shows the first matching envelope that matches the given profiles.
   put         Add the given envelope to the application for the given profiles.
   remove      removes the application envelope for the given profile.
   edit        edits the application envelope for the given profile.
   help        Display help for commands or topics
Run "application help [command]" for command usage.

The global flags are:
   -alsologtostderr=true: log to standard error as well as files
   -log_backtrace_at=:0: when logging hits line file:N, emit a stack trace
   -log_dir=: if non-empty, write log files to this directory
   -logtostderr=false: log to standard error instead of files
   -max_stack_buf_size=4292608: max size in bytes of the buffer to use for logging stack traces
   -stderrthreshold=2: logs at or above this threshold go to stderr
   -v=0: log level for V logs
   -vmodule=: comma-separated list of pattern=N settings for file-filtered logging
   -vv=0: log level for V logs

Application Match

Shows the first matching envelope that matches the given profiles.

Usage:
   application match <application> <profiles>

<application> is the full name of the application.
<profiles> is a comma-separated list of profiles.

Application Put

Add the given envelope to the application for the given profiles.

Usage:
   application put <application> <profiles> <envelope>

<application> is the full name of the application.
<profiles> is a comma-separated list of profiles.
<envelope> is the file that contains a JSON-encoded envelope.

Application Remove

removes the application envelope for the given profile.

Usage:
   application remove <application> <profile>

<application> is the full name of the application.
<profile> is a profile.

Application Edit

edits the application envelope for the given profile.

Usage:
   application edit <application> <profile>

<application> is the full name of the application.
<profile> is a profile.

Application Help

Help with no args displays the usage of the parent command.
Help with args displays the usage of the specified sub-command or help topic.
"help ..." recursively displays help for all commands and topics.

Usage:
   application help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main
