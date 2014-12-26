// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The device tool facilitates interaction with the veyron device manager.

Usage:
   device <command>

The device commands are:
   install     Install the given application.
   start       Start an instance of the given application.
   associate   Tool for creating associations between Vanadium blessings and a
               system account
   claim       Claim the device.
   stop        Stop the given application instance.
   suspend     Suspend the given application instance.
   resume      Resume the given application instance.
   acl         Tool for setting device manager ACLs
   help        Display help for commands or topics
Run "device help [command]" for command usage.

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
 -veyron.vtrace.cache_size=1024
   The number of vtrace traces to store in memory.
 -veyron.vtrace.dump_on_shutdown=false
   If true, dump all stored traces on runtime shutdown.
 -veyron.vtrace.sample_rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -vmodule=
   comma-separated list of pattern=N settings for file-filtered logging

Device Install

Install the given application.

Usage:
   device install <device> <application>

<device> is the veyron object name of the device manager's app service.
<application> is the veyron object name of the application.

Device Start

Start an instance of the given application.

Usage:
   device start <application installation> <grant extension>

<application installation> is the veyron object name of the application
installation from which to start an instance.

<grant extension> is used to extend the default blessing of the current
principal when blessing the app instance.

Device Associate

The associate tool facilitates managing blessing to system account associations.

Usage:
   device associate <command>

The device associate commands are:
   list        Lists the account associations.
   add         Add the listed blessings with the specified system account.
   remove      Removes system accounts associated with the listed blessings.

Device Associate List

Lists all account associations.

Usage:
   device associate list <devicemanager>.

<devicemanager> is the name of the device manager to connect to.

Device Associate Add

Add the listed blessings with the specified system account.

Usage:
   device associate add <devicemanager> <systemName> <blessing>...

<devicemanager> is the name of the device manager to connect to. <systemName> is
the name of an account holder on the local system. <blessing>.. are the
blessings to associate systemAccount with.

Device Associate Remove

Removes system accounts associated with the listed blessings.

Usage:
   device associate remove <devicemanager>  <blessing>...

<devicemanager> is the name of the device manager to connect to. <blessing>...
is a list of blessings.

Device Claim

Claim the device.

Usage:
   device claim <device> <grant extension>

<device> is the veyron object name of the device manager's app service.

<grant extension> is used to extend the default blessing of the current
principal when blessing the app instance.

Device Stop

Stop the given application instance.

Usage:
   device stop <app instance>

<app instance> is the veyron object name of the application instance to stop.

Device Suspend

Suspend the given application instance.

Usage:
   device suspend <app instance>

<app instance> is the veyron object name of the application instance to suspend.

Device Resume

Resume the given application instance.

Usage:
   device resume <app instance>

<app instance> is the veyron object name of the application instance to resume.

Device Acl

The acl tool manages ACLs on the device manger, installations and instances.

Usage:
   device acl <command>

The device acl commands are:
   get         Get ACLs for the given target.
   set         Set ACLs for the given target.

Device Acl Get

Get ACLs for the given target.

Usage:
   device acl get <device manager name>

<device manager name> can be a Vanadium name for a device manager, application
installation or instance.

Device Acl Set

Set ACLs for the given target

Usage:
   device acl set <device manager name>  (<blessing> [!]<tag>(,[!]<tag>)*

<device manager name> can be a Vanadium name for a device manager, application
installation or instance.

<blessing> is a blessing pattern. If the same pattern is repeated multiple times
in the command, then the only the last occurrence will be honored.

<tag> is a subset of defined access types ("Admin", "Read", "Write" etc.). If
the access right is prefixed with a '!' then <blessing> is added to the NotIn
list for that right. Using "^" as a "tag" causes all occurrences of <blessing>
in the current ACL to be cleared.

Examples: set root/self ^ will remove "root/self" from the In and NotIn lists
for all access rights.

set root/self Read,!Write will add "root/self" to the In list for Read access
and the NotIn list for Write access (and remove "root/self" from both the In and
NotIn lists of all other access rights)

Device Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   device help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The device help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
