// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/lib/cmdline/testdata/gendoc.go .

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/user"
	"time"

	_ "v.io/core/veyron/profiles/static"
	vsecurity "v.io/core/veyron/security"
	"v.io/lib/cmdline"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/vom"
)

var (
	// Flags for the "blessself" command
	flagBlessSelfCaveats caveatsFlag
	flagBlessSelfFor     time.Duration

	// Flags for the "bless" command
	flagBlessCaveats        caveatsFlag
	flagBlessFor            time.Duration
	flagBlessRequireCaveats bool
	flagBlessWith           string
	flagBlessRemoteKey      string
	flagBlessRemoteToken    string

	// Flags for the "fork" command
	flagForkCaveats        caveatsFlag
	flagForkFor            time.Duration
	flagForkRequireCaveats bool
	flagForkWith           string

	// Flags for the "seekblessings" command
	flagSeekBlessingsFrom       string
	flagSeekBlessingsSetDefault bool
	flagSeekBlessingsForPeer    string
	flagSeekBlessingsBrowser    bool

	// Flags common to many commands
	flagAddToRoots      bool
	flagCreateOverwrite bool

	// Flags for the "recvblessings" command
	flagRecvBlessingsSetDefault bool
	flagRecvBlessingsForPeer    string

	errNoCaveats = fmt.Errorf("no caveats provided: it is generally dangerous to bless another principal without any caveats as that gives them almost unrestricted access to the blesser's credentials. If you really want to do this, set --require_caveats=false")
	cmdDump      = &cmdline.Command{
		Name:  "dump",
		Short: "Dump out information about the principal",
		Long: `
Prints out information about the principal specified by the environment
that this tool is running in.
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			ctx, shutdown := v23.Init()
			defer shutdown()

			p := v23.GetPrincipal(ctx)
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
be restricted with an expiry caveat specified using the --for flag. Additional
caveats can be added with the --caveat flag.
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

			caveats, err := caveatsFromFlags(flagBlessSelfFor, &flagBlessSelfCaveats)
			if err != nil {
				return err
			}
			ctx, shutdown := v23.Init()
			defer shutdown()
			principal := v23.GetPrincipal(ctx)
			blessing, err := principal.BlessSelf(name, caveats...)
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

The blesser is obtained from the runtime this tool is using. The blessing that
will be extended is the default one from the blesser's store, or specified by
the --with flag. Expiration on the blessing are controlled via the --for flag.
Additional caveats are controlled with the --caveat flag.

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

			ctx, shutdown := v23.Init()
			defer shutdown()

			p := v23.GetPrincipal(ctx)

			var (
				err  error
				with security.Blessings
			)
			if len(flagBlessWith) > 0 {
				if with, err = decodeBlessings(flagBlessWith); err != nil {
					return fmt.Errorf("failed to read blessings from --with=%q: %v", flagBlessWith, err)
				}
			} else {
				with = p.BlessingStore().Default()
			}
			caveats, err := caveatsFromFlags(flagBlessFor, &flagBlessCaveats)
			if err != nil {
				return err
			}
			if !flagBlessRequireCaveats && len(caveats) == 0 {
				caveats = []security.Caveat{security.UnconstrainedUse()}
			}
			if len(caveats) == 0 {
				return errNoCaveats
			}
			tobless, extension := args[0], args[1]
			if (len(flagBlessRemoteKey) == 0) != (len(flagBlessRemoteToken) == 0) {
				return fmt.Errorf("either both --remote_key and --remote_token should be set, or neither should")
			}
			if len(flagBlessRemoteKey) > 0 {
				// Send blessings to a "server" started by a "recvblessings" command
				granter := &granter{p, with, extension, caveats, flagBlessRemoteKey}
				return sendBlessings(ctx, tobless, granter, flagBlessRemoteToken)
			}
			// Blessing a principal whose key is available locally.
			var key security.PublicKey
			if finfo, err := os.Stat(tobless); err == nil && finfo.IsDir() {
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
			ctx, shutdown := v23.Init()
			defer shutdown()
			principal := v23.GetPrincipal(ctx)
			return dumpBlessings(principal.BlessingStore().ForPeer(args...))
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
			ctx, shutdown := v23.Init()
			defer shutdown()
			principal := v23.GetPrincipal(ctx)
			return dumpBlessings(principal.BlessingStore().Default())
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

			ctx, shutdown := v23.Init()
			defer shutdown()

			p := v23.GetPrincipal(ctx)
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

			ctx, shutdown := v23.Init()
			defer shutdown()

			p := v23.GetPrincipal(ctx)
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

			ctx, shutdown := v23.Init()
			defer shutdown()

			p := v23.GetPrincipal(ctx)
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
to the provided directory. The same directory can then be used to set the
VEYRON_CREDENTIALS environment variable for other veyron applications.

The operation fails if the directory already contains a principal. In this case
the --overwrite flag can be provided to clear the directory and write out the
new principal.
`,
		ArgsName: "<directory> <blessing>",
		ArgsLong: `
	<directory> is the directory to which the new principal will be persisted.
	<blessing> is the self-blessed blessing that the principal will be setup to use by default.
	`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("requires exactly two arguments: <directory> and <blessing>, provided %d", len(args))
			}
			dir, name := args[0], args[1]
			if flagCreateOverwrite {
				if err := os.RemoveAll(dir); err != nil {
					return err
				}
			}
			p, err := vsecurity.CreatePersistentPrincipal(dir, nil)
			if err != nil {
				return err
			}
			blessings, err := p.BlessSelf(name)
			if err != nil {
				return fmt.Errorf("BlessSelf(%q) failed: %v", name, err)
			}
			if err := vsecurity.SetDefaultBlessings(p, blessings); err != nil {
				return fmt.Errorf("could not set blessings %v as default: %v", blessings, err)
			}
			return nil
		},
	}

	cmdFork = &cmdline.Command{
		Name:  "fork",
		Short: "Fork a new principal from the principal that this tool is running as and persist it into a directory",
		Long: `
Creates a new principal with a blessing from the principal specified by the
environment that this tool is running in, and writes it out to the provided
directory. The blessing that will be extended is the default one from the
blesser's store, or specified by the --with flag. Expiration on the blessing
are controlled via the --for flag. Additional caveats on the blessing are
controlled with the --caveat flag. The blessing is marked as default and
shareable with all peers on the new principal's blessing store.

The operation fails if the directory already contains a principal. In this case
the --overwrite flag can be provided to clear the directory and write out the
forked principal.
`,
		ArgsName: "<directory> <extension>",
		ArgsLong: `
	<directory> is the directory to which the forked principal will be persisted.
	<extension> is the extension under which the forked principal is blessed.
	`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("requires exactly two arguments: <directory> and <extension>, provided %d", len(args))
			}
			dir, extension := args[0], args[1]

			ctx, shutdown := v23.Init()
			defer shutdown()

			caveats, err := caveatsFromFlags(flagForkFor, &flagForkCaveats)
			if err != nil {
				return err
			}
			if !flagForkRequireCaveats && len(caveats) == 0 {
				caveats = []security.Caveat{security.UnconstrainedUse()}
			}
			if len(caveats) == 0 {
				return errNoCaveats
			}
			var with security.Blessings
			if len(flagForkWith) > 0 {
				if with, err = decodeBlessings(flagForkWith); err != nil {
					return fmt.Errorf("failed to read blessings from --with=%q: %v", flagForkWith, err)
				}
			} else {
				with = v23.GetPrincipal(ctx).BlessingStore().Default()
			}

			if flagCreateOverwrite {
				if err := os.RemoveAll(dir); err != nil {
					return err
				}
			}
			p, err := vsecurity.CreatePersistentPrincipal(dir, nil)
			if err != nil {
				return err
			}

			key := p.PublicKey()
			rp := v23.GetPrincipal(ctx)
			blessings, err := rp.Bless(key, with, extension, caveats[0], caveats[1:]...)
			if err != nil {
				return fmt.Errorf("Bless(%v, %v, %q, ...) failed: %v", key, with, extension, err)
			}
			if err := vsecurity.SetDefaultBlessings(p, blessings); err != nil {
				return fmt.Errorf("could not set blessings %v as default: %v", blessings, err)
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
			ctx, shutdown := v23.Init()
			defer shutdown()

			blessedChan := make(chan string)
			defer close(blessedChan)
			macaroonChan, err := getMacaroonForBlessRPC(flagSeekBlessingsFrom, blessedChan, flagSeekBlessingsBrowser)
			if err != nil {
				return fmt.Errorf("failed to get macaroon from Veyron blesser: %v", err)
			}

			blessings, err := exchangeMacaroonForBlessing(ctx, macaroonChan)
			if err != nil {
				return err
			}
			blessedChan <- fmt.Sprint(blessings)
			// Wait for getTokenForBlessRPC to clean up:
			<-macaroonChan

			p := v23.GetPrincipal(ctx)

			if flagSeekBlessingsSetDefault {
				if err := p.BlessingStore().SetDefault(blessings); err != nil {
					return fmt.Errorf("failed to set blessings %v as default: %v", blessings, err)
				}
			}
			if pattern := security.BlessingPattern(flagSeekBlessingsForPeer); len(pattern) > 0 {
				if _, err := p.BlessingStore().Set(blessings, pattern); err != nil {
					return fmt.Errorf("failed to set blessings %v for peers %v: %v", blessings, pattern, err)
				}
			}
			if flagAddToRoots {
				if err := p.AddToRoots(blessings); err != nil {
					return fmt.Errorf("AddToRoots failed: %v", err)
				}
			}
			fmt.Fprintf(cmd.Stdout(), "Received blessings: %v\n", blessings)
			return nil
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

			ctx, shutdown := v23.Init()
			defer shutdown()

			server, err := v23.NewServer(ctx)
			if err != nil {
				return fmt.Errorf("failed to create server to listen for blessings: %v", err)
			}
			defer server.Stop()
			eps, err := server.Listen(v23.GetListenSpec(ctx))
			if err != nil {
				return fmt.Errorf("failed to setup listening: %v", err)
			}
			var token [24]byte
			if _, err := rand.Read(token[:]); err != nil {
				return fmt.Errorf("unable to generate token: %v", err)
			}

			p := v23.GetPrincipal(ctx)
			service := &recvBlessingsService{
				principal: p,
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
			fmt.Printf("principal bless --remote_key=%v --remote_token=%v %v %v\n", p.PublicKey(), service.token, eps[0].Name(), extension)
			fmt.Println()
			fmt.Println("...waiting for sender..")
			return <-service.notify
		},
	}
)

func main() {
	cmdBlessSelf.Flags.Var(&flagBlessSelfCaveats, "caveat", flagBlessSelfCaveats.usage())
	cmdBlessSelf.Flags.DurationVar(&flagBlessSelfFor, "for", 0, "Duration of blessing validity (zero implies no expiration)")

	cmdFork.Flags.BoolVar(&flagCreateOverwrite, "overwrite", false, "If true, any existing principal data in the directory will be overwritten")
	cmdFork.Flags.Var(&flagForkCaveats, "caveat", flagForkCaveats.usage())
	cmdFork.Flags.DurationVar(&flagForkFor, "for", 0, "Duration of blessing validity (zero implies no expiration caveat)")
	cmdFork.Flags.BoolVar(&flagForkRequireCaveats, "require_caveats", true, "If false, allow blessing without any caveats. This is typically not advised as the principal wielding the blessing will be almost as powerful as its blesser")
	cmdFork.Flags.StringVar(&flagForkWith, "with", "", "Path to file containing blessing to extend")

	cmdBless.Flags.Var(&flagBlessCaveats, "caveat", flagBlessCaveats.usage())
	cmdBless.Flags.DurationVar(&flagBlessFor, "for", 0, "Duration of blessing validity (zero implies no expiration caveat)")
	cmdBless.Flags.BoolVar(&flagBlessRequireCaveats, "require_caveats", true, "If false, allow blessing without any caveats. This is typically not advised as the principal wielding the blessing will be almost as powerful as its blesser")
	cmdBless.Flags.StringVar(&flagBlessWith, "with", "", "Path to file containing blessing to extend")
	cmdBless.Flags.StringVar(&flagBlessRemoteKey, "remote_key", "", "Public key of the remote principal to bless (obtained from the 'recvblessings' command run by the remote principal")
	cmdBless.Flags.StringVar(&flagBlessRemoteToken, "remote_token", "", "Token provided by principal running the 'recvblessings' command")

	cmdSeekBlessings.Flags.StringVar(&flagSeekBlessingsFrom, "from", "https://auth.dev.v.io:8125/google", "URL to use to begin the seek blessings process")
	cmdSeekBlessings.Flags.BoolVar(&flagSeekBlessingsSetDefault, "set_default", true, "If true, the blessings obtained will be set as the default blessing in the store")
	cmdSeekBlessings.Flags.StringVar(&flagSeekBlessingsForPeer, "for_peer", string(security.AllPrincipals), "If non-empty, the blessings obtained will be marked for peers matching this pattern in the store")
	cmdSeekBlessings.Flags.BoolVar(&flagSeekBlessingsBrowser, "browser", true, "If false, the seekblessings command will not open the browser and only print the url to visit.")
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

	root := &cmdline.Command{
		Name:  "principal",
		Short: "Create and manage veyron principals",
		Long: `
The principal tool helps create and manage blessings and the set of trusted
roots bound to a principal.

All objects are printed using base64-VOM-encoding.
`,
		Children: []*cmdline.Command{cmdCreate, cmdFork, cmdSeekBlessings, cmdRecvBlessings, cmdDump, cmdDumpBlessings, cmdBlessSelf, cmdBless, cmdStore},
	}
	os.Exit(root.Main())
}

func decodeBlessings(fname string) (security.Blessings, error) {
	var wire security.WireBlessings
	if err := decode(fname, &wire); err != nil {
		return security.Blessings{}, err
	}
	return security.NewBlessings(wire)
}

func dumpBlessings(blessings security.Blessings) error {
	if blessings.IsZero() {
		return fmt.Errorf("no blessings found")
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
	enc, err := vom.NewEncoder(closer)
	if err != nil {
		return "", err
	}
	if err := enc.Encode(i); err != nil {
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
	dec, err := vom.NewDecoder(bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	return dec.Decode(i)
}

type recvBlessingsService struct {
	principal security.Principal
	notify    chan error
	token     string
}

func (r *recvBlessingsService) Grant(call ipc.ServerCall, token string) error {
	b := call.Blessings()
	if b.IsZero() {
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
		return security.Blessings{}, fmt.Errorf("key mismatch: Remote end has public key %v, want %v", got, g.serverKey)
	}
	return g.p.Bless(server.PublicKey(), g.with, g.extension, g.caveats[0], g.caveats[1:]...)
}
func (*granter) IPCCallOpt() {}

func sendBlessings(ctx *context.T, object string, granter *granter, remoteToken string) error {
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, object, "Grant", []interface{}{remoteToken}, granter)
	if err != nil {
		return fmt.Errorf("failed to start RPC to %q: %v", object, err)
	}
	if err := call.Finish(); err != nil {
		return fmt.Errorf("failed to finish RPC to %q: %v", object, err)
	}
	return nil
}

func caveatsFromFlags(expiry time.Duration, caveatsFlag *caveatsFlag) ([]security.Caveat, error) {
	caveats, err := caveatsFlag.Compile()
	if err != nil {
		return nil, fmt.Errorf("failed to parse caveats: %v", err)
	}
	if expiry > 0 {
		ecav, err := security.ExpiryCaveat(time.Now().Add(expiry))
		if err != nil {
			return nil, fmt.Errorf("failed to create expiration caveat: %v", err)
		}
		caveats = append(caveats, ecav)
	}
	return caveats, nil
}
