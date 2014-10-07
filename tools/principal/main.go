package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"veyron.io/veyron/veyron/lib/cmdline"
	"veyron.io/veyron/veyron/services/identity/util"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
)

var (
	// Flags for the "blessself" command
	flagBlessFor   time.Duration
	flagAddForPeer string

	cmdPrint = &cmdline.Command{
		Name:  "print",
		Short: "Print out information about the provided blessing",
		Long: `
Prints out information about the blessing (typically obtained from this tool)
encoded in the provided file.
`,
		ArgsName: "<file>",
		ArgsLong: `
<file> is the path to a file containing a blessing typically obtained from
this tool. - is used for STDIN.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("requires exactly one argument, <file>, provided %d", len(args))
			}
			blessings, err := decodeBlessings(args[0])
			if err != nil {
				return fmt.Errorf("failed to decode provided blessings: %v", err)
			}
			fmt.Printf("Blessings: %v\n", blessings)
			fmt.Printf("PublicKey: %v\n", blessings.PublicKey())
			return nil
		},
	}

	cmdBlessSelf = &cmdline.Command{
		Name:  "blessself",
		Short: "Generate a self-signed blessing",
		Long: `
Returns a blessing with name <name> and self-signed by the principal
specified by the environment (VEYRON_CREDENTIALS) that this tool is
running in. Optionally, the blessing can be restricted with an expiry
caveat specified using the --for flag.
`,
		ArgsName: "[<name>]",
		ArgsLong: `
<name> is the name used to create the self-signed blessing. If not
specified, a name will be generated based on the hostname of the
machine and the name of the user running this command.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			r := rt.R()
			var name string
			switch len(args) {
			case 0:
				name = defaultBlessingName()
			case 1:
				name = args[0]
			default:
				return fmt.Errorf("requires at most one argument, provided %d", len(args))
			}

			var caveats []security.Caveat
			if flagBlessFor != 0 {
				cav, err := security.ExpiryCaveat(time.Now().Add(flagBlessFor))
				if err != nil {
					return fmt.Errorf("failed to create expiration caveat: %v", err)
				}
				caveats = append(caveats, cav)
			}
			blessing, err := r.Principal().BlessSelf(name, caveats...)
			if err != nil {
				return fmt.Errorf("failed to create self-signed blessing for name %q: %v", name, err)
			}

			return dumpBlessings(blessing)
		},
	}

	cmdForPeer = &cmdline.Command{
		Name:  "store.forpeer",
		Short: "Return blessings marked for the provided peer",
		Long: `
Returns blessings that are marked for the provided peer in the
BlessingStore specified by the environment (VEYRON_CREDENTIALS)
that this tool is running in.
`,
		ArgsName: "[<peer_1> ... <peer_k>]",
		ArgsLong: `
<peer_1> ... <peer_k> are the (human-readable string) blessings bound
to the peer. The returned blessings are marked with a pattern that is
matched by at least one of these. If no arguments are specified,
store.forpeer returns the blessings that are marked for all peers (i.e.,
blessings set on the store with the "..." pattern).
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			return dumpBlessings(rt.R().Principal().BlessingStore().ForPeer(args...))
		},
	}

	cmdDefault = &cmdline.Command{
		Name:  "store.default",
		Short: "Return blessings marked as default",
		Long: `
Returns blessings that are marked as default in the BlessingStore
specified by the environment (VEYRON_CREDENTIALS) that this tool
is running in.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			return dumpBlessings(rt.R().Principal().BlessingStore().Default())
		},
	}
)

func main() {
	if len(os.Getenv("VEYRON_CREDENTIALS")) == 0 {
		// TODO(ataly, ashankar): Handle this case
		fmt.Fprintf(os.Stderr, "ERROR: Please set the VEYRON_CREDENTIALS environment variable\n")
		os.Exit(2)
	}
	rt.Init()
	cmdBlessSelf.Flags.DurationVar(&flagBlessFor, "for", 0*time.Hour, "Expiry time of Blessing (optional)")

	(&cmdline.Command{
		Name:  "principal",
		Short: "Create and manage veyron principals",
		Long: `
The principal tool helps create and manage blessings and the set of trusted
roots bound to a principal.

All objects are printed using base64-VOM-encoding.
`,
		Children: []*cmdline.Command{cmdPrint, cmdBlessSelf, cmdDefault, cmdForPeer},
	}).Main()
}

func decodeBlessings(fname string) (security.Blessings, error) {
	var wire security.WireBlessings
	if err := decode(fname, &wire); err != nil {
		return nil, err
	}
	return security.NewBlessings(wire)
}

func dumpBlessings(blessings security.Blessings) error {
	if blessings == nil {
		return errors.New("no blessings found")
	}
	str, err := util.Base64VomEncode(blessings)
	if err != nil {
		return fmt.Errorf("base64-VOM encoding failed: %v", err)
	}
	fmt.Println(str)
	return nil
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

func defaultBlessingName() string {
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
