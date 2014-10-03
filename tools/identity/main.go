// Below is the output from $(identity help -style=godoc ...)

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
   help        Display help for commands

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
   -clientid=761523829214-4ms7bae18ef47j6590u9ncs19ffuo7b3.apps.googleusercontent.com: OAuth client ID used to make OAuth request for an authorization code
   -for=: Path to file containing identity to bless (or - for STDIN)
   -from=/proxy.envyor.com:8101/identity/veyron-test/google: Object name of Veyron service running the identity.OAuthBlesser service to seek blessings from

Identity Help

Help displays usage descriptions for this command, or usage descriptions for
sub-commands.

Usage:
   identity help [flags] [command ...]

[command ...] is an optional sequence of commands to display detailed usage.
The special-case "help ..." recursively displays help for all commands.

The help flags are:
   -style=text: The formatting style for help output, either "text" or "godoc".
*/
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"veyron.io/veyron/veyron/lib/cmdline"
	"veyron.io/veyron/veyron/services/identity"
	"veyron.io/veyron/veyron/services/identity/util"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	// Flags for the "bless" command
	flagBlessWith string
	flagBlessFor  time.Duration

	// Flags for the "seekblessing" command
	flagSeekBlessingFor           string
	flagSeekBlessingOAuthClientID string
	flagSeekBlessingFrom          string

	cmdPrint = &cmdline.Command{
		Name:  "print",
		Short: "Print out information about the provided identity",
		Long: `
Print dumps out information about the identity encoded in the provided file,
or if no filename is provided, then the identity that would be used by binaries
started in the same environment.
`,
		ArgsName: "[<file>]",
		ArgsLong: `
<file> is the path to a file containing a base64-encoded, VOM encoded identity,
typically obtained from this tool. - is used for STDIN and an empty string
implies the identity encoded in the environment.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("require at most one argument, <file>, provided %d", len(args))
			}
			id := rt.R().Identity()
			if len(args) == 1 {
				if err := decode(args[0], &id); err != nil {
					return err
				}
			}
			fmt.Println("Name     : ", id.PublicID())
			fmt.Printf("Go Type  : %T\n", id)
			fmt.Printf("PublicKey: %v\n", id.PublicID().PublicKey())
			fmt.Println("Any caveats in the identity are not printed")
			return nil
		},
	}

	cmdGenerate = &cmdline.Command{
		Name:  "generate",
		Short: "Generate an identity with a newly minted private key",
		Long: `
Generate a new private key and create an identity that binds <name> to
this key.

Since the generated identity has a newly minted key, it will be typically
unusable at other veyron services as those services have placed no trust
in this key. In such cases, you likely want to seek a blessing for this
generated identity using the 'bless' command.
`,
		ArgsName: "[<name>]",
		ArgsLong: `
<name> is the name to bind the newly minted private key to. If not specified,
a name will be generated based on the hostname of the machine and the name of
the user running this command.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			r := rt.R()
			var name string
			switch len(args) {
			case 0:
				name = defaultIdentityName()
			case 1:
				name = args[0]
			default:
				return fmt.Errorf("require at most one argument, provided %d", len(args))
			}
			id, err := r.NewIdentity(name)
			if err != nil {
				return fmt.Errorf("NewIdentity(%q) failed: %v", name, err)
			}
			output, err := util.Base64VomEncode(id)
			if err != nil {
				return fmt.Errorf("failed to encode identity: %v", err)
			}
			fmt.Println(output)
			return nil
		},
	}

	cmdBless = &cmdline.Command{
		Name:  "bless",
		Short: "Bless another identity with your own",
		Long: `
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
`,
		ArgsName: "<file> <name>",
		ArgsLong: `
<file> is the name of the file containing a base64-vom encoded security.PublicID
or security.PrivateID

<name> is the name to use for the blessing.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("expected exactly two arguments (<file> and <name>), got %d", len(args))
			}
			blesser := rt.R().Identity()
			if len(flagBlessWith) > 0 {
				if err := decode(flagBlessWith, &blesser); err != nil {
					return err
				}
			}
			name := args[1]
			var blessee security.PublicID
			var private security.PrivateID
			encoded, err := read(args[0])
			if err != nil {
				return err
			}
			if util.Base64VomDecode(encoded, &blessee); err != nil || blessee == nil {
				if err := util.Base64VomDecode(encoded, &private); err != nil || private == nil {
					return fmt.Errorf("failed to extract security.PublicID or security.PrivateID: (%v, %v)", private, err)
				}
				blessee = private.PublicID()
			}
			blessed, err := blesser.Bless(blessee, name, flagBlessFor, nil)
			if err != nil {
				return err
			}
			var object interface{} = blessed
			if private != nil {
				object, err = private.Derive(blessed)
				if err != nil {
					return err
				}
			}
			output, err := util.Base64VomEncode(object)
			if err != nil {
				return err
			}
			fmt.Println(output)
			return nil
		},
	}

	cmdSeekBlessing = &cmdline.Command{
		Name:  "seekblessing",
		Short: "Seek a blessing from the default veyron identity provider",
		Long: `
Seeks a blessing from a default, hardcoded Veyron identity provider which
requires the caller to first authenticate with Google using OAuth. Simply
run the command to see what happens.

The blessing is sought for the identity that this tool is using. An alternative
can be provided with the --for flag.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			r := rt.R()
			id := r.Identity()

			if len(flagSeekBlessingFor) > 0 {
				if err := decode(flagSeekBlessingFor, &id); err != nil {
					return err
				}
				var err error
				if r, err = rt.New(veyron2.RuntimeID(id)); err != nil {
					return err
				}
			}

			blessedChan := make(chan string)
			defer close(blessedChan)
			macaroonChan, err := getMacaroonForBlessRPC(flagSeekBlessingFrom, blessedChan)
			if err != nil {
				return fmt.Errorf("failed to get authorization code from Google: %v", err)
			}
			macaroon := <-macaroonChan
			service := <-macaroonChan

			ctx, cancel := r.NewContext().WithTimeout(time.Minute)
			defer cancel()

			wait := time.Second
			const maxWait = 20 * time.Second
			var reply vdlutil.Any
			for {
				blesser, err := identity.BindMacaroonBlesser(service, r.Client())
				if err == nil {
					reply, err = blesser.Bless(ctx, macaroon)
				}
				if err != nil {
					vlog.Infof("Failed to get blessing from %q: %v, will try again in %v", service, err, wait)
					time.Sleep(wait)
					if wait = wait + 2*time.Second; wait > maxWait {
						wait = maxWait
					}
					continue
				}
				blessed, ok := reply.(security.PublicID)
				if !ok {
					return fmt.Errorf("received %T, want security.PublicID", reply)
				}
				if id, err = id.Derive(blessed); err != nil {
					return fmt.Errorf("received incompatible blessing from %q: %v", service, err)
				}
				output, err := util.Base64VomEncode(id)
				if err != nil {
					return fmt.Errorf("failed to encode blessing: %v", err)
				}
				fmt.Println(output)
				blessedChan <- fmt.Sprint(blessed)
				// Wait for getTokenForBlessRPC to clean up:
				<-macaroonChan
				return nil
			}
		},
	}
)

func main() {
	rt.Init()
	cmdBless.Flags.StringVar(&flagBlessWith, "with", "", "Path to file containing identity to bless with (or - for STDIN)")
	cmdBless.Flags.DurationVar(&flagBlessFor, "for", 365*24*time.Hour, "Expiry time of blessing (defaults to 1 year)")
	cmdSeekBlessing.Flags.StringVar(&flagSeekBlessingFor, "for", "", "Path to file containing identity to bless (or - for STDIN)")
	cmdSeekBlessing.Flags.StringVar(&flagSeekBlessingFrom, "from", "http://proxy.envyor.com:8125/google", "URL to use to begin the seek blessings process")

	(&cmdline.Command{
		Name:  "identity",
		Short: "Create and manage veyron identities",
		Long: `
The identity tool helps create and manage keys and blessings that are used for
identification in veyron.
`,
		Children: []*cmdline.Command{cmdPrint, cmdGenerate, cmdBless, cmdSeekBlessing},
	}).Main()
}

func read(fname string) (string, error) {
	if len(fname) == 0 {
		return "", nil
	}
	f := os.Stdin
	if fname != "-" {
		var err error
		if f, err = os.Open(fname); err != nil {
			return "", fmt.Errorf("failed to open %q: %v", fname, err)
		}
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, f); err != nil {
		return "", fmt.Errorf("failed to read %q: %v", fname, err)
	}
	return buf.String(), nil
}

func decode(fname string, val interface{}) error {
	str, err := read(fname)
	if err != nil {
		return err
	}
	if err := util.Base64VomDecode(str, val); err != nil || val == nil {
		return fmt.Errorf("failed to decode %q: %v", fname, err)
	}
	return nil
}

func defaultIdentityName() string {
	var name string
	if user, _ := user.Current(); user != nil && len(user.Username) > 0 {
		name = user.Username
	} else {
		name = "anonymous"
	}
	if host, _ := os.Hostname(); len(host) > 0 {
		name = name + "@" + host
	}
	return name
}
