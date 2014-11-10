package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"

	"veyron.io/veyron/veyron/lib/cmdline"
	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/services/identity"
)

var (
	// Flags for the "blessself" command
	flagBlessSelfFor time.Duration

	// Flags for the "bless" command
	flagBlessFor  time.Duration
	flagBlessWith string

	// Flags for the "seekblessings" command
	flagSeekBlessingsFrom       string
	flagSeekBlessingsSetDefault bool
	flagSeekBlessingsForPeer    string

	// Flag for the create command
	flagCreateOverwrite bool

	// Flags common to many commands
	flagAddToRoots bool

	cmdDump = &cmdline.Command{
		Name:  "dump",
		Short: "Dump out information about the principal",
		Long: `
Prints out information about the principal specified by the environment
that this tool is running in.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			p := rt.Init().Principal()
			fmt.Printf("Public key : %v\n", p.PublicKey())
			fmt.Println("---------------- BlessingStore ----------------")
			fmt.Printf("%v", p.BlessingStore().DebugString())
			fmt.Println("---------------- BlessingRoots ----------------")
			fmt.Printf("%v", p.Roots().DebugString())
			return nil
		},
	}

	cmdDumpBlessings = &cmdline.Command{
		Name:  "dumpblessings",
		Short: "Dump out information about the provided blessings",
		Long: `
Prints out information about the blessings (typically obtained from this tool)
encoded in the provided file.
`,
		ArgsName: "<file>",
		ArgsLong: `
<file> is the path to a file containing blessings typically obtained from
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
			wire := security.MarshalBlessings(blessings)
			fmt.Printf("Blessings          : %v\n", blessings)
			fmt.Printf("PublicKey          : %v\n", blessings.PublicKey())
			fmt.Printf("Certificate chains : %d\n", len(wire.CertificateChains))
			for idx, chain := range wire.CertificateChains {
				fmt.Printf("Chain #%d (%d certificates). Root certificate public key: %v\n", idx, len(chain), rootkey(chain))
				for certidx, cert := range chain {
					fmt.Printf("  Certificate #%d: %v with ", certidx, cert.Extension)
					switch n := len(cert.Caveats); n {
					case 1:
						fmt.Printf("1 caveat")
					default:
						fmt.Printf("%d caveats", n)
					}
					fmt.Println("")
					for cavidx, cav := range cert.Caveats {
						fmt.Printf("    (%d) %v\n", cavidx, &cav)
					}
				}
			}
			return nil
		},
	}

	cmdBlessSelf = &cmdline.Command{
		Name:  "blessself",
		Short: "Generate a self-signed blessing",
		Long: `
Returns a blessing with name <name> and self-signed by the principal specified
by the environment that this tool is running in. Optionally, the blessing can
be restricted with an expiry caveat specified using the --for flag.
`,
		ArgsName: "[<name>]",
		ArgsLong: `
<name> is the name used to create the self-signed blessing. If not
specified, a name will be generated based on the hostname of the
machine and the name of the user running this command.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
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
			if flagBlessSelfFor != 0 {
				cav, err := security.ExpiryCaveat(time.Now().Add(flagBlessSelfFor))
				if err != nil {
					return fmt.Errorf("failed to create expiration Caveat: %v", err)
				}
				caveats = append(caveats, cav)
			}
			blessing, err := rt.Init().Principal().BlessSelf(name, caveats...)
			if err != nil {
				return fmt.Errorf("failed to create self-signed blessing for name %q: %v", name, err)
			}

			return dumpBlessings(blessing)
		},
	}

	cmdBless = &cmdline.Command{
		Name:  "bless",
		Short: "Bless another principal",
		Long: `
	Returns a set of blessings obtained when one principal blesses another.

	The blesser is obtained from the runtime this tool is running as.
	The principal to be blessed is specified as either a path to the
	directory of the other principal, or the filename (- for STDIN)
	of any other blessing of that principal.
	The blessing that the blesser uses (i.e., which is extended to create
	the blessing) is the default one from the blessers store, or specified
	via the --with flag.  The blessing is valid only for the duration
	specified in --for.

	For example, let's say a principal with the default blessing "alice"
	wants to bless another principal as "alice/bob", the invocation would
	be:
	VEYRON_CREDENTIALS=<path to alice> principal bless <path to bob> friend
	`,
		ArgsName: "<principal to bless> <extension>",
		ArgsLong: `
	<principal to bless> represents the principal to be blessed (i.e.,
	whose public key will be provided with a name).  This can either be a
	path to a file containing any other set of blessings of that principal
	(or - for STDIN) or the path to the directory of that principal.

	<extension> is the string extension that will be applied to create the
	blessing.
	`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("require exactly two arguments, provided %d", len(args))
			}
			p := rt.Init().Principal()

			var with security.Blessings
			var err error
			if len(flagBlessWith) > 0 {
				if with, err = decodeBlessings(flagBlessWith); err != nil {
					return fmt.Errorf("failed to read blessings from --with=%q: %v", flagBlessWith, err)
				}
			} else {
				with = p.BlessingStore().Default()
			}

			var key security.PublicKey
			tobless, extension := args[0], args[1]
			if finfo, err := os.Stat(tobless); err == nil && finfo.IsDir() {
				// TODO(suharshs,ashankar,ataly): How should we make an ecrypted pk... or is that up to the agent?
				other, err := vsecurity.LoadPersistentPrincipal(tobless, nil)
				if err != nil {
					if other, err = vsecurity.CreatePersistentPrincipal(tobless, nil); err != nil {
						return fmt.Errorf("failed to read principal in directory %q: %v", tobless, err)
					}
				}
				key = other.PublicKey()
			} else if other, err := decodeBlessings(tobless); err != nil {
				return fmt.Errorf("failed to decode blessings in %q: %v", tobless, err)
			} else {
				key = other.PublicKey()
			}

			caveat, err := security.ExpiryCaveat(time.Now().Add(flagBlessFor))
			if err != nil {
				return fmt.Errorf("failed to create ExpirtyCaveat: %v", err)
			}

			blessings, err := p.Bless(key, with, extension, caveat)
			if err != nil {
				return fmt.Errorf("Bless(%v, %v, %q, ExpiryCaveat(%v)) failed: %v", key, with, extension, flagBlessFor, err)
			}
			return dumpBlessings(blessings)
		},
	}

	cmdStoreForPeer = &cmdline.Command{
		Name:  "forpeer",
		Short: "Return blessings marked for the provided peer",
		Long: `
Returns blessings that are marked for the provided peer in the
BlessingStore specified by the environment that this tool is
running in.
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
			return dumpBlessings(rt.Init().Principal().BlessingStore().ForPeer(args...))
		},
	}

	cmdStoreDefault = &cmdline.Command{
		Name:  "default",
		Short: "Return blessings marked as default",
		Long: `
Returns blessings that are marked as default in the BlessingStore specified by
the environment that this tool is running in.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			return dumpBlessings(rt.Init().Principal().BlessingStore().Default())
		},
	}

	cmdStoreSet = &cmdline.Command{
		Name:  "set",
		Short: "Set provided blessings for peer",
		Long: `
Marks the provided blessings to be shared with the provided peers on the
BlessingStore specified by the environment that this tool is running in.

'set b pattern' marks the intention to reveal b to peers who
present blessings of their own matching 'pattern'.

'set nil pattern' can be used to remove the blessings previously
associated with the pattern (by a prior 'set' command).

It is an error to call 'store.set' with blessings whose public
key does not match the public key of this principal specified
by the environment.
`,
		ArgsName: "<file> <pattern>",
		ArgsLong: `
<file> is the path to a file containing a blessing typically obtained
from this tool. - is used for STDIN.

<pattern> is the BlessingPattern used to identify peers with whom this
blessing can be shared with.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("requires exactly two arguments <file>, <pattern>, provided %d", len(args))
			}
			blessings, err := decodeBlessings(args[0])
			if err != nil {
				return fmt.Errorf("failed to decode provided blessings: %v", err)
			}
			pattern := security.BlessingPattern(args[1])
			p := rt.Init().Principal()
			if _, err := p.BlessingStore().Set(blessings, pattern); err != nil {
				return fmt.Errorf("failed to set blessings %v for peers %v: %v", blessings, pattern, err)
			}
			if flagAddToRoots {
				if err := p.AddToRoots(blessings); err != nil {
					return fmt.Errorf("AddToRoots failed: %v", err)
				}
			}
			return nil
		},
	}

	cmdStoreSetDefault = &cmdline.Command{
		Name:  "setdefault",
		Short: "Set provided blessings as default",
		Long: `
Sets the provided blessings as default in the BlessingStore specified by the
environment that this tool is running in.

It is an error to call 'store.setdefault' with blessings whose public key does
not match the public key of the principal specified by the environment.
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
			p := rt.Init().Principal()
			if err := p.BlessingStore().SetDefault(blessings); err != nil {
				return fmt.Errorf("failed to set blessings %v as default: %v", blessings, err)
			}
			if flagAddToRoots {
				if err := p.AddToRoots(blessings); err != nil {
					return fmt.Errorf("AddToRoots failed: %v", err)
				}
			}
			return nil
		},
	}

	cmdCreate = &cmdline.Command{
		Name:  "create",
		Short: "Create a new principal and persist it into a directory",
		Long: `
Creates a new principal with a single self-blessed blessing and writes it out
to the provided directory. The same directory can be used to set the VEYRON_CREDENTIALS
environment variables for other veyron applications.

The operation fails if the directory already contains a principal. In this case
the --overwrite flag can be provided to clear the directory and write out a
new principal.
`,
		ArgsName: "<directory> <blessing>",
		ArgsLong: `
	<directory> is the directory to which the principal will be persisted.
	<blessing> is the self-blessed blessing that the principal will be setup to use by default.
	`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("requires exactly two arguments: <directory> and <blessing>, provided %d", len(args))
			}
			dir, name := args[0], args[1]
			// TODO(suharshs,ashankar,ataly): How should we make an ecrypted pk... or is that up to the agent?
			var (
				p   security.Principal
				err error
			)
			if flagCreateOverwrite {
				if err = os.RemoveAll(dir); err != nil {
					return err
				}
				p, err = vsecurity.CreatePersistentPrincipal(dir, nil)
			} else {
				p, err = vsecurity.CreatePersistentPrincipal(dir, nil)
			}
			if err != nil {
				return err
			}
			blessings, err := p.BlessSelf(name)
			if err != nil {
				return fmt.Errorf("BlessSelf(%q) failed: %v", name, err)
			}
			if err := p.BlessingStore().SetDefault(blessings); err != nil {
				return fmt.Errorf("BlessingStore.SetDefault(%v) failed: %v", blessings, err)
			}
			if _, err := p.BlessingStore().Set(blessings, security.AllPrincipals); err != nil {
				return fmt.Errorf("BlessingStore.Set(%v, %q) failed: %v", blessings, security.AllPrincipals, err)
			}
			if err := p.AddToRoots(blessings); err != nil {
				return fmt.Errorf("AddToRoots(%v) failed: %v", blessings, err)
			}
			return nil
		},
	}

	cmdSeekBlessings = &cmdline.Command{
		Name:  "seekblessings",
		Short: "Seek blessings from a web-based Veyron blesser",
		Long: `
Seeks blessings from a web-based Veyron blesser which
requires the caller to first authenticate with Google using OAuth. Simply
run the command to see what happens.

The blessings are sought for the principal specified by the environment that
this tool is running in.

The blessings obtained are set as default, unless a --skip_set_default flag
is provided, and are also set for sharing with all peers, unless a more
specific peer pattern is provided using the --for_peer flag.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			// Initialize the runtime first so that any local errors are reported
			// before the HTTP roundtrips for obtaining the macaroon begin.
			r := rt.Init()
			blessedChan := make(chan string)
			defer close(blessedChan)
			macaroonChan, err := getMacaroonForBlessRPC(flagSeekBlessingsFrom, blessedChan)
			if err != nil {
				return fmt.Errorf("failed to get macaroon from Veyron blesser: %v", err)
			}
			macaroon := <-macaroonChan
			service := <-macaroonChan
			ctx, cancel := r.NewContext().WithTimeout(time.Minute)
			defer cancel()

			var reply security.WireBlessings
			reply, err = identity.MacaroonBlesserClient(service).Bless(ctx, macaroon)
			if err != nil {
				return fmt.Errorf("failed to get blessing from %q: %v", service, err)
			}
			blessings, err := security.NewBlessings(reply)
			if err != nil {
				return fmt.Errorf("failed to construct Blessings object from response: %v", err)
			}
			blessedChan <- fmt.Sprint(blessings)
			// Wait for getTokenForBlessRPC to clean up:
			<-macaroonChan

			if flagSeekBlessingsSetDefault {
				if err := r.Principal().BlessingStore().SetDefault(blessings); err != nil {
					return fmt.Errorf("failed to set blessings %v as default: %v", blessings, err)
				}
			}
			if pattern := security.BlessingPattern(flagSeekBlessingsForPeer); len(pattern) > 0 {
				if _, err := r.Principal().BlessingStore().Set(blessings, pattern); err != nil {
					return fmt.Errorf("failed to set blessings %v for peers %v: %v", blessings, pattern, err)
				}
			}
			if flagAddToRoots {
				if err := r.Principal().AddToRoots(blessings); err != nil {
					return fmt.Errorf("AddToRoots failed: %v", err)
				}
			}
			return dumpBlessings(blessings)
		},
	}
)

func main() {
	cmdBlessSelf.Flags.DurationVar(&flagBlessSelfFor, "for", 0, "Duration of blessing validity (zero means no that the blessing is always valid)")
	cmdBless.Flags.DurationVar(&flagBlessFor, "for", time.Minute, "Duration of blessing validity")
	cmdBless.Flags.StringVar(&flagBlessWith, "with", "", "Path to file containing blessing to extend. ")
	cmdSeekBlessings.Flags.StringVar(&flagSeekBlessingsFrom, "from", "https://proxy.envyor.com:8125/google", "URL to use to begin the seek blessings process")
	cmdSeekBlessings.Flags.BoolVar(&flagSeekBlessingsSetDefault, "set_default", true, "If true, the blessings obtained will be set as the default blessing in the store")
	cmdSeekBlessings.Flags.StringVar(&flagSeekBlessingsForPeer, "for_peer", string(security.AllPrincipals), "If non-empty, the blessings obtained will be marked for peers matching this pattern in the store")
	cmdSeekBlessings.Flags.BoolVar(&flagAddToRoots, "add_to_roots", true, "If true, the root certificate of the blessing will be added to the principal's set of recognized root certificates")
	cmdStoreSet.Flags.BoolVar(&flagAddToRoots, "add_to_roots", true, "If true, the root certificate of the blessing will be added to the principal's set of recognized root certificates")
	cmdStoreSetDefault.Flags.BoolVar(&flagAddToRoots, "add_to_roots", true, "If true, the root certificate of the blessing will be added to the principal's set of recognized root certificates")
	cmdCreate.Flags.BoolVar(&flagCreateOverwrite, "overwrite", false, "If true, any existing principal data in the directory will be overwritten")

	cmdStore := &cmdline.Command{
		Name:  "store",
		Short: "Manipulate and inspect the principal's blessing store",
		Long: `
Commands to manipulate and inspect the blessing store of the principal.

All blessings are printed to stdout using base64-VOM-encoding
`,
		Children: []*cmdline.Command{cmdStoreDefault, cmdStoreSetDefault, cmdStoreForPeer, cmdStoreSet},
	}

	(&cmdline.Command{
		Name:  "principal",
		Short: "Create and manage veyron principals",
		Long: `
The principal tool helps create and manage blessings and the set of trusted
roots bound to a principal.

All objects are printed using base64-VOM-encoding.
`,
		Children: []*cmdline.Command{cmdCreate, cmdSeekBlessings, cmdDump, cmdDumpBlessings, cmdBlessSelf, cmdBless, cmdStore},
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
	str, err := base64VomEncode(blessings)
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
	if err := base64VomDecode(str, val); err != nil || val == nil {
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

func rootkey(chain []security.Certificate) string {
	if len(chain) == 0 {
		return "<empty certificate chain>"
	}
	key, err := security.UnmarshalPublicKey(chain[0].PublicKey)
	if err != nil {
		return fmt.Sprintf("<invalid PublicKey: %v>", err)
	}
	return fmt.Sprintf("%v", key)
}

func base64VomEncode(i interface{}) (string, error) {
	buf := &bytes.Buffer{}
	closer := base64.NewEncoder(base64.URLEncoding, buf)
	if err := vom.NewEncoder(closer).Encode(i); err != nil {
		return "", err
	}
	// Must close the base64 encoder to flush out any partially written
	// blocks.
	if err := closer.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func base64VomDecode(s string, i interface{}) error {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return vom.NewDecoder(bytes.NewBuffer(b)).Decode(i)
}
