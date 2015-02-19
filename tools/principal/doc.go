// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The principal tool helps create and manage blessings and the set of trusted
roots bound to a principal.

All objects are printed using base64-VOM-encoding.

Usage:
   principal <command>

The principal commands are:
   create        Create a new principal and persist it into a directory
   fork          Fork a new principal from the principal that this tool is
                 running as and persist it into a directory
   seekblessings Seek blessings from a web-based Veyron blessing service
   recvblessings Receive blessings sent by another principal and use them as the
                 default
   dump          Dump out information about the principal
   dumpblessings Dump out information about the provided blessings
   blessself     Generate a self-signed blessing
   bless         Bless another principal
   store         Manipulate and inspect the principal's blessing store
   help          Display help for commands or topics
Run "principal help [command]" for command usage.

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
   explicitly specify the runtime acl as a JSON-encoded access.TaggedACLMap.
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

Principal Create

Creates a new principal with a single self-blessed blessing and writes it out to
the provided directory. The same directory can then be used to set the
VEYRON_CREDENTIALS environment variable for other veyron applications.

The operation fails if the directory already contains a principal. In this case
the --overwrite flag can be provided to clear the directory and write out the
new principal.

Usage:
   principal create [flags] <directory> <blessing>

	<directory> is the directory to which the new principal will be persisted.
	<blessing> is the self-blessed blessing that the principal will be setup to use by default.

The principal create flags are:
 -overwrite=false
   If true, any existing principal data in the directory will be overwritten

Principal Fork

Creates a new principal with a blessing from the principal specified by the
environment that this tool is running in, and writes it out to the provided
directory. The blessing that will be extended is the default one from the
blesser's store, or specified by the --with flag. Expiration on the blessing are
controlled via the --for flag. Additional caveats on the blessing are controlled
with the --caveat flag. The blessing is marked as default and shareable with all
peers on the new principal's blessing store.

The operation fails if the directory already contains a principal. In this case
the --overwrite flag can be provided to clear the directory and write out the
forked principal.

Usage:
   principal fork [flags] <directory> <extension>

	<directory> is the directory to which the forked principal will be persisted.
	<extension> is the extension under which the forked principal is blessed.

The principal fork flags are:
 -caveat=[]
   "package/path".CaveatName:VDLExpressionParam to attach to this blessing
 -for=0
   Duration for which the forked blessing is valid (zero means no that the
   blessing is always valid)
 -overwrite=false
   If true, any existing principal data in the directory will be overwritten
 -with=
   Path to file containing blessing to extend

Principal Seekblessings

Seeks blessings from a web-based Veyron blesser which requires the caller to
first authenticate with Google using OAuth. Simply run the command to see what
happens.

The blessings are sought for the principal specified by the environment that
this tool is running in.

The blessings obtained are set as default, unless the --set_default flag is set
to true, and are also set for sharing with all peers, unless a more specific
peer pattern is provided using the --for_peer flag.

Usage:
   principal seekblessings [flags]

The principal seekblessings flags are:
 -add_to_roots=true
   If true, the root certificate of the blessing will be added to the
   principal's set of recognized root certificates
 -browser=true
   If false, the seekblessings command will not open the browser and only print
   the url to visit.
 -for_peer=...
   If non-empty, the blessings obtained will be marked for peers matching this
   pattern in the store
 -from=https://auth.dev.v.io:8125/google
   URL to use to begin the seek blessings process
 -set_default=true
   If true, the blessings obtained will be set as the default blessing in the
   store

Principal Recvblessings

Allow another principal (likely a remote process) to bless this one.

This command sets up the invoker (this process) to wait for a blessing from
another invocation of this tool (remote process) and prints out the command to
be run as the remote principal.

The received blessings are set as default, unless the --set_default flag is set
to true, and are also set for sharing with all peers, unless a more specific
peer pattern is provided using the --for_peer flag.

TODO(ashankar,cnicolaou): Make this next paragraph possible! Requires the
ability to obtain the proxied endpoint.

Typically, this command should require no arguments. However, if the sender and
receiver are on different network domains, it may make sense to use the
--veyron.proxy flag:
    principal --veyron.proxy=proxy recvblessings

The command to be run at the sender is of the form:
    principal bless --remote_key=KEY --remote_token=TOKEN ADDRESS

The --remote_key flag is used to by the sender to "authenticate" the receiver,
ensuring it blesses the intended recipient and not any attacker that may have
taken over the address.

The --remote_token flag is used by the sender to authenticate itself to the
receiver. This helps ensure that the receiver rejects blessings from senders who
just happened to guess the network address of the 'recvblessings' invocation.

Usage:
   principal recvblessings [flags]

The principal recvblessings flags are:
 -for_peer=...
   If non-empty, the blessings received will be marked for peers matching this
   pattern in the store
 -set_default=true
   If true, the blessings received will be set as the default blessing in the
   store

Principal Dump

Prints out information about the principal specified by the environment that
this tool is running in.

Usage:
   principal dump

Principal Dumpblessings

Prints out information about the blessings (typically obtained from this tool)
encoded in the provided file.

Usage:
   principal dumpblessings <file>

<file> is the path to a file containing blessings typically obtained from this
tool. - is used for STDIN.

Principal Blessself

Returns a blessing with name <name> and self-signed by the principal specified
by the environment that this tool is running in. Optionally, the blessing can be
restricted with an expiry caveat specified using the --for flag. Additional
caveats can be added with the --caveat flag.

Usage:
   principal blessself [flags] [<name>]

<name> is the name used to create the self-signed blessing. If not specified, a
name will be generated based on the hostname of the machine and the name of the
user running this command.

The principal blessself flags are:
 -caveat=[]
   "package/path".CaveatName:VDLExpressionParam to attach to this blessing
 -for=0
   Duration of blessing validity (zero means no that the blessing is always
   valid)

Principal Bless

Bless another principal.

The blesser is obtained from the runtime this tool is using. The blessing that
will be extended is the default one from the blesser's store, or specified by
the --with flag. Expiration on the blessing are controlled via the --for flag.
Additional caveats are controlled with the --caveat flag.

For example, let's say a principal "alice" wants to bless another principal
"bob" as "alice/friend", the invocation would be:
    VEYRON_CREDENTIALS=<path to alice> principal bless <path to bob> friend
and this will dump the blessing to STDOUT.

With the --remote_key and --remote_token flags, this command can be used to
bless a principal on a remote machine as well. In this case, the blessing is not
dumped to STDOUT but sent to the remote end. Use 'principal help recvblessings'
for more details on that.

Usage:
   principal bless [flags] <principal to bless> <extension>

<principal to bless> represents the principal to be blessed (i.e., whose public
key will be provided with a name).  This can be either: (a) The directory
containing credentials for that principal, OR (b) The filename (- for STDIN)
containing any other blessings of that
    principal,
OR (c) The object name produced by the 'recvblessings' command of this tool
    running on behalf of another principal (if the --remote_key and
    --remote_token flags are specified).

<extension> is the string extension that will be applied to create the blessing.

The principal bless flags are:
 -caveat=[]
   "package/path".CaveatName:VDLExpressionParam to attach to this blessing
 -for=1m0s
   Duration of blessing validity
 -remote_key=
   Public key of the remote principal to bless (obtained from the
   'recvblessings' command run by the remote principal
 -remote_token=
   Token provided by principal running the 'recvblessings' command
 -with=
   Path to file containing blessing to extend

Principal Store

Commands to manipulate and inspect the blessing store of the principal.

All blessings are printed to stdout using base64-VOM-encoding

Usage:
   principal store <command>

The principal store commands are:
   default     Return blessings marked as default
   setdefault  Set provided blessings as default
   forpeer     Return blessings marked for the provided peer
   set         Set provided blessings for peer
   addtoroots  Add provided blessings to root set

Principal Store Default

Returns blessings that are marked as default in the BlessingStore specified by
the environment that this tool is running in.

Usage:
   principal store default

Principal Store Setdefault

Sets the provided blessings as default in the BlessingStore specified by the
environment that this tool is running in.

It is an error to call 'store.setdefault' with blessings whose public key does
not match the public key of the principal specified by the environment.

Usage:
   principal store setdefault [flags] <file>

<file> is the path to a file containing a blessing typically obtained from this
tool. - is used for STDIN.

The principal store setdefault flags are:
 -add_to_roots=true
   If true, the root certificate of the blessing will be added to the
   principal's set of recognized root certificates

Principal Store Forpeer

Returns blessings that are marked for the provided peer in the BlessingStore
specified by the environment that this tool is running in.

Usage:
   principal store forpeer [<peer_1> ... <peer_k>]

<peer_1> ... <peer_k> are the (human-readable string) blessings bound to the
peer. The returned blessings are marked with a pattern that is matched by at
least one of these. If no arguments are specified, store.forpeer returns the
blessings that are marked for all peers (i.e., blessings set on the store with
the "..." pattern).

Principal Store Set

Marks the provided blessings to be shared with the provided peers on the
BlessingStore specified by the environment that this tool is running in.

'set b pattern' marks the intention to reveal b to peers who present blessings
of their own matching 'pattern'.

'set nil pattern' can be used to remove the blessings previously associated with
the pattern (by a prior 'set' command).

It is an error to call 'store.set' with blessings whose public key does not
match the public key of this principal specified by the environment.

Usage:
   principal store set [flags] <file> <pattern>

<file> is the path to a file containing a blessing typically obtained from this
tool. - is used for STDIN.

<pattern> is the BlessingPattern used to identify peers with whom this blessing
can be shared with.

The principal store set flags are:
 -add_to_roots=true
   If true, the root certificate of the blessing will be added to the
   principal's set of recognized root certificates

Principal Store Addtoroots

Adds the provided blessings to the set of trusted roots for this principal.

'addtoroots b' adds blessings b to the trusted root set.

For example, to make the principal in credentials directory A trust the root of
the default blessing in credentials directory B:
  principal -veyron.credentials=B bless A some_extension |
  principal -veyron.credentials=A store addtoroots -

The extension 'some_extension' has no effect in the command above.

Usage:
   principal store addtoroots <file>

<file> is the path to a file containing a blessing typically obtained from this
tool. - is used for STDIN.

Principal Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   principal help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The principal help flags are:
 -style=text
   The formatting style for help output, either "text" or "godoc".
*/
package main
