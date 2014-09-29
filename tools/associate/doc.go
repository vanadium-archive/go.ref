// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The associate tool facilitates creating blessing to system account associations.

Usage:
   associate <command>

The associate commands are:
   list        Lists the account associations.
   add         Associate the listed blessings with the specified system account
   remove      Removes system accounts associated with the listed blessings.
   help        Display help for commands or topics
Run "associate help [command]" for command usage.

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

Associate List

Lists all account associations 

Usage:
   associate list <nodemanager>.

<nodemanager> is the name of the node manager to connect to.

Associate Add

Associate the listed blessings with the specified system account

Usage:
   associate add <nodemanager> <systemName> <blessing>...

<identify specifier>... is a list of 1 or more identify specifications
<systemName> is the name of an account holder on the local system
<blessing>.. are the blessings to associate systemAccount with

Associate Remove

Removes system accounts associated with the listed blessings.

Usage:
   associate remove <nodemanager>  <blessing>...

<nodemanager> is the node manager to connect to
<blessing>... is a list of blessings.

Associate Help

Help with no args displays the usage of the parent command.
Help with args displays the usage of the specified sub-command or help topic.
"help ..." recursively displays help for all commands and topics.

Usage:
   associate help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main
