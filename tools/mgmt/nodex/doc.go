// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The nodex tool facilitates interaction with the veyron node manager.

Usage:
   nodex <command>

The nodex commands are:
   install     Install the given application.
   start       Start an instance of the given application.
   associate   Tool for creating associations between Vanadium blessings and a
               system account
   claim       Claim the node.
   stop        Stop the given application instance.
   suspend     Suspend the given application instance.
   resume      Resume the given application instance.
   acl         Tool for setting node manager ACLs
   help        Display help for commands or topics
Run "nodex help [command]" for command usage.

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

Nodex Install

Install the given application.

Usage:
   nodex install <node> <application>

<node> is the veyron object name of the node manager's app service.
<application> is the veyron object name of the application.

Nodex Start

Start an instance of the given application.

Usage:
   nodex start <application installation> <grant extension>

<application installation> is the veyron object name of the application
installation from which to start an instance.

<grant extension> is used to extend the default blessing of the current
principal when blessing the app instance.

Nodex Associate

The associate tool facilitates managing blessing to system account associations.

Usage:
   nodex associate <command>

The nodex associate commands are:
   list        Lists the account associations.
   add         Add the listed blessings with the specified system account.
   remove      Removes system accounts associated with the listed blessings.

Nodex Associate List

Lists all account associations.

Usage:
   nodex associate list <nodemanager>.

<nodemanager> is the name of the node manager to connect to.

Nodex Associate Add

Add the listed blessings with the specified system account.

Usage:
   nodex associate add <nodemanager> <systemName> <blessing>...

<nodemanager> is the name of the node manager to connect to. <systemName> is the
name of an account holder on the local system. <blessing>.. are the blessings to
associate systemAccount with.

Nodex Associate Remove

Removes system accounts associated with the listed blessings.

Usage:
   nodex associate remove <nodemanager>  <blessing>...

<nodemanager> is the name of the node manager to connect to. <blessing>... is a
list of blessings.

Nodex Claim

Claim the node.

Usage:
   nodex claim <node> <grant extension>

<node> is the veyron object name of the node manager's app service.

<grant extension> is used to extend the default blessing of the current
principal when blessing the app instance.

Nodex Stop

Stop the given application instance.

Usage:
   nodex stop <app instance>

<app instance> is the veyron object name of the application instance to stop.

Nodex Suspend

Suspend the given application instance.

Usage:
   nodex suspend <app instance>

<app instance> is the veyron object name of the application instance to suspend.

Nodex Resume

Resume the given application instance.

Usage:
   nodex resume <app instance>

<app instance> is the veyron object name of the application instance to resume.

Nodex Acl

The acl tool manages ACLs on the node manger, installations and instances.

Usage:
   nodex acl <command>

The nodex acl commands are:
   get         Get ACLs for the given target.
   set         Set ACLs for the given target.

Nodex Acl Get

Get ACLs for the given target.

Usage:
   nodex acl get <node manager name>

<node manager name> can be a Vanadium name for a node manager, application
installation or instance.

Nodex Acl Set

Set ACLs for the given target

Usage:
   nodex acl set <node manager name>  (<blessing> [!]<label>)...

<node manager name> can be a Vanadium name for a node manager, application
installation or instance.

<blessing> is a blessing pattern.

<label> is a character sequence defining a set of rights: some subset of the
defined standard Vanadium labels of XRWADM where X is resolve, R is read, W for
write, A for admin, D for debug and M is for monitoring. By default, the
combination of <blessing>, <label> replaces whatever entry is present in the
ACL's In field for the <blessing> but it can instead be added to the NotIn field
by prefacing <label> with a '!' character. Use the <label> of 0 to clear the
label.

For example: root/self !0 will clear the NotIn field for blessingroot/self.

Nodex Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   nodex help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The nodex help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
