// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The identity tool helps create and manage keys and blessings that are used for
identification in veyron.

Usage:
   identity <command>

The identity commands are:
   print       Print out information about the provided identity
   generate    Generate an identity with a newly minted private key
   bless       Bless another identity with your own
   seekblessing Seek a blessing from the default veyron identity provider
   help        Display help for commands or topics
Run "identity help [command]" for command usage.

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

Identity Print

Print dumps out information about the identity encoded in the provided file,
or if no filename is provided, then the identity that would be used by binaries
started in the same environment.

Usage:
   identity print [<file>]

<file> is the path to a file containing a base64-encoded, VOM encoded identity,
typically obtained from this tool. - is used for STDIN and an empty string
implies the identity encoded in the environment.

Identity Generate

Generate a new private key and create an identity that binds <name> to
this key.

Since the generated identity has a newly minted key, it will be typically
unusable at other veyron services as those services have placed no trust
in this key. In such cases, you likely want to seek a blessing for this
generated identity using the 'bless' command.

Usage:
   identity generate [<name>]

<name> is the name to bind the newly minted private key to. If not specified,
a name will be generated based on the hostname of the machine and the name of
the user running this command.

Identity Bless

Bless uses the identity of the tool (either from an environment variable or
explicitly specified using --with) to bless another identity encoded in a
file (or STDIN). No caveats are applied to this blessing other than expiration,
which is specified with --for.

The output consists of a base64-vom encoded security.PrivateID or security.PublicID,
depending on what was provided as input.

For example, if the tool has an identity veyron/user/device, then
bless /tmp/blessee batman
will generate a blessing with the name veyron/user/device/batman

The identity of the tool can be specified with the --with flag:
bless --with /tmp/id /tmp/blessee batman

Usage:
   identity bless [flags] <file> <name>

<file> is the name of the file containing a base64-vom encoded security.PublicID
or security.PrivateID

<name> is the name to use for the blessing.

The bless flags are:
   -for=8760h0m0s: Expiry time of blessing (defaults to 1 year)
   -with=: Path to file containing identity to bless with (or - for STDIN)

Identity Seekblessing

Seeks a blessing from a default, hardcoded Veyron identity provider which
requires the caller to first authenticate with Google using OAuth. Simply
run the command to see what happens.

The blessing is sought for the identity that this tool is using. An alternative
can be provided with the --for flag.

Usage:
   identity seekblessing [flags]

The seekblessing flags are:
   -for=: Path to file containing identity to bless (or - for STDIN)
   -from=https://proxy.envyor.com:8125/google: URL to use to begin the seek blessings process

Identity Help

Help with no args displays the usage of the parent command.
Help with args displays the usage of the specified sub-command or help topic.
"help ..." recursively displays help for all commands and topics.

Usage:
   identity help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main
