// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The device tool facilitates interaction with the veyron device manager.

Usage:
   device <command>

The device commands are:
   install       Install the given application.
   install-local Install the given application from the local system.
   uninstall     Uninstall the given application installation.
   start         Start an instance of the given application.
   associate     Tool for creating associations between Vanadium blessings and a
                 system account
   describe      Describe the device.
   claim         Claim the device.
   stop          Stop the given application instance.
   suspend       Suspend the given application instance.
   resume        Resume the given application instance.
   revert        Revert the device manager or application
   update        Update the device manager or application
   updateall     Update all installations/instances of an application
   debug         Debug the device.
   acl           Tool for setting device manager AccessLists
   publish       Publish the given application(s).
   help          Display help for commands or topics
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
 -veyron.acl.file=map[]
   specify an acl file as <name>:<aclfile>
 -veyron.acl.literal=
   explicitly specify the runtime acl as a JSON-encoded access.Permissions.
   Overrides all --veyron.acl.file flags.
 -veyron.credentials=
   directory to use for storing security credentials
 -veyron.namespace.root=[/ns.dev.v.io:8101]
   local namespace root; can be repeated to provided multiple roots
 -veyron.proxy=
   object name of proxy service to use to export services across network
   boundaries
 -veyron.tcp.address=
   address to listen on
 -veyron.tcp.protocol=wsh
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

Device Install

Install the given application.

Usage:
   device install [flags] <device> <application>

<device> is the veyron object name of the device manager's app service.

<application> is the veyron object name of the application.

The device install flags are:
 -config={}
   JSON-encoded device.Config object, of the form:
   '{"flag1":"value1","flag2":"value2"}'
 -packages={}
   JSON-encoded application.Packages object, of the form:
   '{"pkg1":{"File":"object name 1"},"pkg2":{"File":"object name 2"}}'

Device Install-Local

Install the given application, specified using a local path.

Usage:
   device install-local [flags] <device> <title> [ENV=VAL ...] binary [--flag=val ...] [PACKAGES path ...]

<device> is the veyron object name of the device manager's app service.

<title> is the app title.

This is followed by an arbitrary number of environment variable settings, the
local path for the binary to install, and arbitrary flag settings and args.
Optionally, this can be followed by 'PACKAGES' and a list of local files and
directories to be installed as packages for the app

The device install-local flags are:
 -config={}
   JSON-encoded device.Config object, of the form:
   '{"flag1":"value1","flag2":"value2"}'
 -packages={}
   JSON-encoded application.Packages object, of the form:
   '{"pkg1":{"File":"local file path1"},"pkg2":{"File":"local file path 2"}}'

Device Uninstall

Uninstall the given application installation.

Usage:
   device uninstall <installation>

<installation> is the veyron object name of the application installation to
uninstall.

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

Device Describe

Describe the device.

Usage:
   device describe <device>

<device> is the veyron object name of the device manager's device service.

Device Claim

Claim the device.

Usage:
   device claim <device> <grant extension> <pairing token> <device publickey>

<device> is the veyron object name of the device manager's device service.

<grant extension> is used to extend the default blessing of the current
principal when blessing the app instance.

<pairing token> is a token that the device manager expects to be replayed during
a claim operation on the device.

<device publickey> is the marshalled public key of the device manager we are
claiming.

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

Device Revert

Revert the device manager or application to its previous version

Usage:
   device revert <object>

<object> is the veyron object name of the device manager or application
installation to revert.

Device Update

Update the device manager or application

Usage:
   device update <object>

<object> is the veyron object name of the device manager or application
installation or instance to update.

Device Updateall

Given a name that can refer to an app instance or app installation or app or all
apps on a device, updates all installations and instances under that name

Usage:
   device updateall <object name>

<object name> is the veyron object name to update, as follows:

<devicename>/apps/apptitle/installationid/instanceid: updates the given
instance, suspending/resuming it if running

<devicename>/apps/apptitle/installationid: updates the given installation and
then all its instances

<devicename>/apps/apptitle: updates all installations for the given app

<devicename>/apps: updates all apps on the device

Device Debug

Debug the device.

Usage:
   device debug <device>

<device> is the veyron object name of an app installation or instance.

Device Acl

The acl tool manages AccessLists on the device manger, installations and
instances.

Usage:
   device acl <command>

The device acl commands are:
   get         Get AccessLists for the given target.
   set         Set AccessLists for the given target.

Device Acl Get

Get AccessLists for the given target.

Usage:
   device acl get <device manager name>

<device manager name> can be a Vanadium name for a device manager, application
installation or instance.

Device Acl Set

Set AccessLists for the given target

Usage:
   device acl set [flags] <device manager name>  (<blessing> [!]<tag>(,[!]<tag>)*

<device manager name> can be a Vanadium name for a device manager, application
installation or instance.

<blessing> is a blessing pattern. If the same pattern is repeated multiple times
in the command, then the only the last occurrence will be honored.

<tag> is a subset of defined access types ("Admin", "Read", "Write" etc.). If
the access right is prefixed with a '!' then <blessing> is added to the NotIn
list for that right. Using "^" as a "tag" causes all occurrences of <blessing>
in the current AccessList to be cleared.

Examples: set root/self ^ will remove "root/self" from the In and NotIn lists
for all access rights.

set root/self Read,!Write will add "root/self" to the In list for Read access
and the NotIn list for Write access (and remove "root/self" from both the In and
NotIn lists of all other access rights)

The device acl set flags are:
 -f=false
   Instead of making the AccessLists additive, do a complete replacement based
   on the specified settings.

Device Publish

Publishes the given application(s) to the binary and application servers. The
binaries should be in $VANADIUM_ROOT/release/go/bin/[<GOOS>_<GOARCH>]. The
binary is published as <binserv>/<binary name>/<GOOS>-<GOARCH>/<TIMESTAMP>. The
application envelope is published as <appserv>/<binary name>/0. Optionally, adds
blessing patterns to the Read and Resolve AccessLists.

Usage:
   device publish [flags] <binary name> ...

The device publish flags are:
 -appserv=applicationd
   Name of application service.
 -binserv=binaryd
   Name of binary service.
 -goarch=${GOARCH}
   GOARCH for application.
 -goos=${GOOS}
   GOOS for application.
 -readers=dev.v.io
   If non-empty, comma-separated blessing patterns to add to Read and Resolve
   AccessList.

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
 -style=default
   The formatting style for help output, either "default" or "godoc".
*/
package main
