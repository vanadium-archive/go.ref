package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	"veyron.io/lib/cmdline"
	profile "veyron.io/veyron/veyron/profiles/static"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/services/identity"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

var (
	// Flags for the "blessself" command
	flagBlessSelfFor time.Duration

	// Flags for the "bless" command
	flagBlessFor         time.Duration
	flagBlessWith        string
	flagBlessRemoteKey   string
	flagBlessRemoteToken string

	// Flags for the "seekblessings" command
	flagSeekBlessingsFrom       string
	flagSeekBlessingsSetDefault bool
	flagSeekBlessingsForPeer    string

	// Flag for the create command
	flagCreateOverwrite bool

	// Flags common to many commands
	flagAddToRoots bool

	// Flags for the "recvblessings" command
	flagRecvBlessingsSetDefault bool
	flagRecvBlessingsForPeer    string

	cmdDump = &cmdline.Command{
		Name:  "dump",
		Short: "Dump out information about the principal",
		Long: `
Prints out information about the principal specified by the environment
that this tool is running in.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			p := runtime.Principal()
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

			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			blessing, err := runtime.Principal().BlessSelf(name, caveats...)
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
Bless another principal.

The blesser is obtained from the runtime this tool is using.  The blessing that
will be extended is the default one from the blesser's store, or specified by
the --with flag. Caveats on the blessing are controlled via the --for flag.

For example, let's say a principal "alice" wants to bless another principal "bob"
as "alice/friend", the invocation would be:
    VEYRON_CREDENTIALS=<path to alice> principal bless <path to bob> friend
and this will dump the blessing to STDOUT.

With the --remote_key and --remote_token flags, this command can be used to
bless a principal on a remote machine as well. In this case, the blessing is
not dumped to STDOUT but sent to the remote end. Use 'principal help
recvblessings' for more details on that.
`,
		ArgsName: "<principal to bless> <extension>",
		ArgsLong: `
<principal to bless> represents the principal to be blessed (i.e., whose public
key will be provided with a name).  This can be either:
(a) The directory containing credentials for that principal,
OR
(b) The filename (- for STDIN) containing any other blessings of that
    principal,
OR
(c) The object name produced by the 'recvblessings' command of this tool
    running on behalf of another principal (if the --remote_key and
    --remote_token flags are specified).

<extension> is the string extension that will be applied to create the
blessing.
	`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("require exactly two arguments, provided %d", len(args))
			}

			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			p := runtime.Principal()

			var with security.Blessings
			var caveats []security.Caveat
			if len(flagBlessWith) > 0 {
				if with, err = decodeBlessings(flagBlessWith); err != nil {
					return fmt.Errorf("failed to read blessings from --with=%q: %v", flagBlessWith, err)
				}
			} else {
				with = p.BlessingStore().Default()
			}

			if c, err := security.ExpiryCaveat(time.Now().Add(flagBlessFor)); err != nil {
				return fmt.Errorf("failed to create ExpiryCaveat: %v", err)
			} else {
				caveats = append(caveats, c)
			}
			// TODO(ashankar,ataly,suharshs): Work out how to add additional caveats, like maybe
			// revocation, method etc.
			tobless, extension := args[0], args[1]
			if (len(flagBlessRemoteKey) == 0) != (len(flagBlessRemoteToken) == 0) {
				return fmt.Errorf("either both --remote_key and --remote_token should be set, or neither should")
			}
			if len(flagBlessRemoteKey) > 0 {
				// Send blessings to a "server" started by a "recvblessings" command
				granter := &granter{p, with, extension, caveats, flagBlessRemoteKey}
				return sendBlessings(runtime, tobless, granter, flagBlessRemoteToken)
			}
			// Blessing a principal whose key is available locally.
			var key security.PublicKey
			if finfo, err := os.Stat(tobless); err == nil && finfo.IsDir() {
				// TODO(suharshs,ashankar,ataly): How should we make an encrypted pk... or is that up to the agent?
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

			blessings, err := p.Bless(key, with, extension, caveats[0], caveats[1:]...)
			if err != nil {
				return fmt.Errorf("Bless(%v, %v, %q, ...) failed: %v", key, with, extension, err)
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
			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			return dumpBlessings(runtime.Principal().BlessingStore().ForPeer(args...))
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
			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			return dumpBlessings(runtime.Principal().BlessingStore().Default())
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

			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			p := runtime.Principal()
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

	cmdStoreAddToRoots = &cmdline.Command{
		Name:  "addtoroots",
		Short: "Add provided blessings to root set",
		Long: `
Adds the provided blessings to the set of trusted roots for this principal.

'addtoroots b' adds blessings b to the trusted root set.

For example, to make the principal in credentials directory A trust the
root of the default blessing in credentials directory B:
  principal -veyron.credentials=B bless A some_extension |
  principal -veyron.credentials=A store addtoroots -

The extension 'some_extension' has no effect in the command above.
`,
		ArgsName: "<file>",
		ArgsLong: `
<file> is the path to a file containing a blessing typically obtained
from this tool. - is used for STDIN.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("requires exactly one argument <file>, provided %d", len(args))
			}
			blessings, err := decodeBlessings(args[0])
			if err != nil {
				return fmt.Errorf("failed to decode provided blessings: %v", err)
			}

			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			p := runtime.Principal()
			if err := p.AddToRoots(blessings); err != nil {
				return fmt.Errorf("AddToRoots failed: %v", err)
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

			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			p := runtime.Principal()
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
		Short: "Seek blessings from a web-based Veyron blessing service",
		Long: `
Seeks blessings from a web-based Veyron blesser which
requires the caller to first authenticate with Google using OAuth. Simply
run the command to see what happens.

The blessings are sought for the principal specified by the environment that
this tool is running in.

The blessings obtained are set as default, unless the --set_default flag is
set to true, and are also set for sharing with all peers, unless a more
specific peer pattern is provided using the --for_peer flag.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			// Initialize the runtime first so that any local errors are reported
			// before the HTTP roundtrips for obtaining the macaroon begin.
			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			blessedChan := make(chan string)
			defer close(blessedChan)
			macaroonChan, err := getMacaroonForBlessRPC(flagSeekBlessingsFrom, blessedChan)
			if err != nil {
				return fmt.Errorf("failed to get macaroon from Veyron blesser: %v", err)
			}
			macaroon := <-macaroonChan
			service := <-macaroonChan
			ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
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
				if err := runtime.Principal().BlessingStore().SetDefault(blessings); err != nil {
					return fmt.Errorf("failed to set blessings %v as default: %v", blessings, err)
				}
			}
			if pattern := security.BlessingPattern(flagSeekBlessingsForPeer); len(pattern) > 0 {
				if _, err := runtime.Principal().BlessingStore().Set(blessings, pattern); err != nil {
					return fmt.Errorf("failed to set blessings %v for peers %v: %v", blessings, pattern, err)
				}
			}
			if flagAddToRoots {
				if err := runtime.Principal().AddToRoots(blessings); err != nil {
					return fmt.Errorf("AddToRoots failed: %v", err)
				}
			}
			return dumpBlessings(blessings)
		},
	}

	cmdRecvBlessings = &cmdline.Command{
		Name:  "recvblessings",
		Short: "Receive blessings sent by another principal and use them as the default",
		Long: `
Allow another principal (likely a remote process) to bless this one.

This command sets up the invoker (this process) to wait for a blessing
from another invocation of this tool (remote process) and prints out the
command to be run as the remote principal.

The received blessings are set as default, unless the --set_default flag is
set to true, and are also set for sharing with all peers, unless a more
specific peer pattern is provided using the --for_peer flag.

TODO(ashankar,cnicolaou): Make this next paragraph possible! Requires
the ability to obtain the proxied endpoint.

Typically, this command should require no arguments.
However, if the sender and receiver are on different network domains, it may
make sense to use the --veyron.proxy flag:
    principal --veyron.proxy=proxy recvblessings

The command to be run at the sender is of the form:
    principal bless --remote_key=KEY --remote_token=TOKEN ADDRESS

The --remote_key flag is used to by the sender to "authenticate" the receiver,
ensuring it blesses the intended recipient and not any attacker that may have
taken over the address.

The --remote_token flag is used by the sender to authenticate itself to the
receiver. This helps ensure that the receiver rejects blessings from senders
who just happened to guess the network address of the 'recvblessings'
invocation.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("command accepts no arguments")
			}

			runtime, err := rt.New()
			if err != nil {
				panic(err)
			}
			defer runtime.Cleanup()

			server, err := runtime.NewServer()
			if err != nil {
				return fmt.Errorf("failed to create server to listen for blessings: %v", err)
			}
			defer server.Stop()
			ep, err := server.Listen(profile.ListenSpec)
			if err != nil {
				return fmt.Errorf("failed to setup listening: %v", err)
			}
			var token [24]byte
			if _, err := rand.Read(token[:]); err != nil {
				return fmt.Errorf("unable to generate token: %v", err)
			}
			service := &recvBlessingsService{
				principal: runtime.Principal(),
				token:     base64.URLEncoding.EncodeToString(token[:]),
				notify:    make(chan error),
			}
			if err := server.Serve("", service, allowAnyone{}); err != nil {
				return fmt.Errorf("failed to setup service: %v", err)
			}
			// Proposed name:
			extension := fmt.Sprintf("extension%d", int(token[0])<<16|int(token[1])<<8|int(token[2]))
			fmt.Println("Run the following command on behalf of the principal that will send blessings:")
			fmt.Println("You may want to adjust flags affecting the caveats on this blessing, for example using")
			fmt.Println("the --for flag, or change the extension to something more meaningful")
			fmt.Println()
			fmt.Printf("principal bless --remote_key=%v --remote_token=%v %v %v\n", runtime.Principal().PublicKey(), service.token, naming.JoinAddressName(ep.String(), ""), extension)
			fmt.Println()
			fmt.Println("...waiting for sender..")
			return <-service.notify
		},
	}
)

func main() {
	cmdBlessSelf.Flags.DurationVar(&flagBlessSelfFor, "for", 0, "Duration of blessing validity (zero means no that the blessing is always valid)")

	cmdBless.Flags.DurationVar(&flagBlessFor, "for", time.Minute, "Duration of blessing validity")
	cmdBless.Flags.StringVar(&flagBlessWith, "with", "", "Path to file containing blessing to extend")
	cmdBless.Flags.StringVar(&flagBlessRemoteKey, "remote_key", "", "Public key of the remote principal to bless (obtained from the 'recvblessings' command run by the remote principal")
	cmdBless.Flags.StringVar(&flagBlessRemoteToken, "remote_token", "", "Token provided by principal running the 'recvblessings' command")

	cmdSeekBlessings.Flags.StringVar(&flagSeekBlessingsFrom, "from", "https://auth.dev.v.io:8125/google", "URL to use to begin the seek blessings process")
	cmdSeekBlessings.Flags.BoolVar(&flagSeekBlessingsSetDefault, "set_default", true, "If true, the blessings obtained will be set as the default blessing in the store")
	cmdSeekBlessings.Flags.StringVar(&flagSeekBlessingsForPeer, "for_peer", string(security.AllPrincipals), "If non-empty, the blessings obtained will be marked for peers matching this pattern in the store")
	cmdSeekBlessings.Flags.BoolVar(&flagAddToRoots, "add_to_roots", true, "If true, the root certificate of the blessing will be added to the principal's set of recognized root certificates")

	cmdStoreSet.Flags.BoolVar(&flagAddToRoots, "add_to_roots", true, "If true, the root certificate of the blessing will be added to the principal's set of recognized root certificates")

	cmdStoreSetDefault.Flags.BoolVar(&flagAddToRoots, "add_to_roots", true, "If true, the root certificate of the blessing will be added to the principal's set of recognized root certificates")

	cmdCreate.Flags.BoolVar(&flagCreateOverwrite, "overwrite", false, "If true, any existing principal data in the directory will be overwritten")

	cmdRecvBlessings.Flags.BoolVar(&flagRecvBlessingsSetDefault, "set_default", true, "If true, the blessings received will be set as the default blessing in the store")
	cmdRecvBlessings.Flags.StringVar(&flagRecvBlessingsForPeer, "for_peer", string(security.AllPrincipals), "If non-empty, the blessings received will be marked for peers matching this pattern in the store")

	cmdStore := &cmdline.Command{
		Name:  "store",
		Short: "Manipulate and inspect the principal's blessing store",
		Long: `
Commands to manipulate and inspect the blessing store of the principal.

All blessings are printed to stdout using base64-VOM-encoding
`,
		Children: []*cmdline.Command{cmdStoreDefault, cmdStoreSetDefault, cmdStoreForPeer, cmdStoreSet, cmdStoreAddToRoots},
	}

	(&cmdline.Command{
		Name:  "principal",
		Short: "Create and manage veyron principals",
		Long: `
The principal tool helps create and manage blessings and the set of trusted
roots bound to a principal.

All objects are printed using base64-VOM-encoding.
`,
		Children: []*cmdline.Command{cmdCreate, cmdSeekBlessings, cmdRecvBlessings, cmdDump, cmdDumpBlessings, cmdBlessSelf, cmdBless, cmdStore},
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
	str, err := base64VomEncode(security.MarshalBlessings(blessings))
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

type recvBlessingsService struct {
	principal security.Principal
	notify    chan error
	token     string
}

func (r *recvBlessingsService) Grant(call ipc.ServerCall, token string) error {
	b := call.Blessings()
	if b == nil {
		return fmt.Errorf("no blessings granted by sender")
	}
	if len(token) != len(r.token) {
		// A timing attack can be used to figure out the length
		// of the token, but then again, so can looking at the
		// source code. So, it's okay.
		return fmt.Errorf("blessings received from unexpected sender")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) != 1 {
		return fmt.Errorf("blessings received from unexpected sender")
	}
	if flagRecvBlessingsSetDefault {
		if err := r.principal.BlessingStore().SetDefault(b); err != nil {
			return fmt.Errorf("failed to set blessings %v as default: %v", b, err)
		}
	}
	if pattern := security.BlessingPattern(flagRecvBlessingsForPeer); len(pattern) > 0 {
		if _, err := r.principal.BlessingStore().Set(b, pattern); err != nil {
			return fmt.Errorf("failed to set blessings %v for peers %v: %v", b, pattern, err)
		}
	}
	if flagAddToRoots {
		if err := r.principal.AddToRoots(b); err != nil {
			return fmt.Errorf("failed to add blessings to recognized roots: %v", err)
		}
	}
	fmt.Println("Received blessings:", b)
	r.notify <- nil
	return nil
}

type allowAnyone struct{}

func (allowAnyone) Authorize(security.Context) error { return nil }

type granter struct {
	p         security.Principal
	with      security.Blessings
	extension string
	caveats   []security.Caveat
	serverKey string
}

func (g *granter) Grant(server security.Blessings) (security.Blessings, error) {
	if got := fmt.Sprintf("%v", server.PublicKey()); got != g.serverKey {
		// If the granter returns an error, the IPC framework should
		// abort the RPC before sending the request to the server.
		// Thus, there is no concern about leaking the token to an
		// imposter server.
		return nil, fmt.Errorf("key mismatch: Remote end has public key %v, want %v", got, g.serverKey)
	}
	return g.p.Bless(server.PublicKey(), g.with, g.extension, g.caveats[0], g.caveats[1:]...)
}
func (*granter) IPCCallOpt() {}

func sendBlessings(r veyron2.Runtime, object string, granter *granter, remoteToken string) error {
	call, err := r.Client().StartCall(r.NewContext(), object, "Grant", []interface{}{remoteToken}, granter)
	if err != nil {
		return fmt.Errorf("failed to start RPC to %q: %v", object, err)
	}
	if ierr := call.Finish(&err); ierr != nil {
		return fmt.Errorf("failed to finish RPC to %q: %v", object, ierr)
	}
	return err
}
