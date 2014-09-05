package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"veyron/lib/cmdline"
	"veyron/services/identity"
	"veyron/services/identity/util"

	"veyron2"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/vdl/vdlutil"
	"veyron2/vlog"
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
				return fmt.Errorf("veyron2.Runtime.NewIdentity(%q) failed: %v", name, err)
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
		ArgsName: "<file> <name> [--with=<file>]",
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
			authcodeChan, err := getOAuthAuthorizationCodeFromGoogle(flagSeekBlessingOAuthClientID, blessedChan)
			if err != nil {
				return fmt.Errorf("failed to get authorization code from Google: %v", err)
			}
			redirectURL := <-authcodeChan
			authcode := <-authcodeChan

			ctx, cancel := r.NewContext().WithTimeout(time.Minute)
			defer cancel()

			wait := time.Second
			const maxWait = 20 * time.Second
			var reply vdlutil.Any
			for {
				blesser, err := identity.BindOAuthBlesser(flagSeekBlessingFrom, r.Client())
				if err == nil {

					reply, err = blesser.BlessUsingAuthorizationCode(ctx, authcode, redirectURL)
				}
				if err != nil {
					vlog.Infof("Failed to get blessing from %q: %v, will try again in %v", flagSeekBlessingFrom, err, wait)
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
					return fmt.Errorf("received incompatible blessing from %q: %v", flagSeekBlessingFrom, err)
				}
				output, err := util.Base64VomEncode(id)
				if err != nil {
					return fmt.Errorf("failed to encode blessing: %v", err)
				}
				fmt.Println(output)
				blessedChan <- fmt.Sprint(blessed)
				// Wait for getOAuthAuthenticationCodeFromGoogle to clean up:
				<-authcodeChan
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
	cmdSeekBlessing.Flags.StringVar(&flagSeekBlessingOAuthClientID, "clientid", "761523829214-4ms7bae18ef47j6590u9ncs19ffuo7b3.apps.googleusercontent.com", "OAuth client ID used to make OAuth request for an authorization code")
	cmdSeekBlessing.Flags.StringVar(&flagSeekBlessingFrom, "from", "/proxy.envyor.com:8101/identity/veyron-test/google", "Object name of Veyron service running the identity.OAuthBlesser service to seek blessings from")

	(&cmdline.Command{
		Name:  "identity",
		Short: "Create an manage veyron identities",
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
