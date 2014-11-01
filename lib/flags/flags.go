package flags

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"veyron.io/veyron/veyron/lib/flags/consts"
)

// FlagGroup is the type for identifying groups of related flags.
type FlagGroup int

const (
	// Runtime identifies the flags and associated environment variables
	// used by the Vanadium process runtime. Namely:
	// --veyron.namespace.root (which may be repeated to supply multiple values)
	// --veyron.credentials
	Runtime FlagGroup = iota
	// Listen identifies the flags typically required to configure
	// ipc.ListenSpec. Namely:
	// --veyron.tcp.protocol
	// --veyron.tcp.address
	// --veyron.proxy
	Listen
)

const defaultNamespaceRoot = "/proxy.envyor.com:8101"

// Flags represents the set of flag groups created by a call to
// CreateAndRegister.
type Flags struct {
	FlagSet *flag.FlagSet
	groups  map[FlagGroup]interface{}
}

type namespaceRootFlagVar struct {
	isSet bool // is true when a flag have has been explicitly set.
	roots []string
}

func (nsr *namespaceRootFlagVar) String() string {
	return fmt.Sprintf("%v", nsr.roots)
}

func (nsr *namespaceRootFlagVar) Set(v string) error {
	if !nsr.isSet {
		// override the default value and
		nsr.isSet = true
		nsr.roots = []string{}
	}
	nsr.roots = append(nsr.roots, v)
	return nil
}

// RuntimeFlags contains the values of the Runtime flag group.
type RuntimeFlags struct {
	// NamespaceRoots may be initialized by NAMESPACE_ROOT* enivornment
	// variables as well as --veyron.namespace.root. The command line
	// will override the environment.
	NamespaceRoots []string // TODO(cnicolaou): provide flag.Value impl

	// Credentials may be initialized by the the VEYRON_CREDENTIALS
	// environment variable. The command line will override the environment.
	Credentials string // TODO(cnicolaou): provide flag.Value impl

	namespaceRootsFlag namespaceRootFlagVar
}

// ListenFlags contains the values of the Listen flag group.
type ListenFlags struct {
	ListenProtocol TCPProtocolFlag
	ListenAddress  IPHostPortFlag
	ListenProxy    string
}

// createAndRegisterRuntimeFlags creates and registers the RuntimeFlags
// group with the supplied flag.FlagSet.
func createAndRegisterRuntimeFlags(fs *flag.FlagSet) *RuntimeFlags {
	f := &RuntimeFlags{}
	roots, creds := readEnv()
	if len(roots) == 0 {
		f.namespaceRootsFlag.roots = []string{defaultNamespaceRoot}
	} else {
		f.namespaceRootsFlag.roots = roots
	}
	fs.Var(&f.namespaceRootsFlag, "veyron.namespace.root", "local namespace root; can be repeated to provided multiple roots")
	fs.StringVar(&f.Credentials, "veyron.credentials", creds, "directory to use for storing security credentials")
	return f
}

// createAndRegisterListenFlags creates and registers the ListenFlags
// group with the supplied flag.FlagSet.
func createAndRegisterListenFlags(fs *flag.FlagSet) *ListenFlags {
	f := &ListenFlags{}
	f.ListenProtocol = TCPProtocolFlag{"tcp"}
	f.ListenAddress = IPHostPortFlag{Port: "0"}
	fs.Var(&f.ListenProtocol, "veyron.tcp.protocol", "protocol to listen with")
	fs.Var(&f.ListenAddress, "veyron.tcp.address", "address to listen on")
	fs.StringVar(&f.ListenProxy, "veyron.proxy", "", "object name of proxy service to use to export services across network boundaries")
	return f
}

// CreateAndRegister creates a new set of flag groups as specified by the
// supplied flag group parameters and registers them with the supplied
// flag.Flagset.
func CreateAndRegister(fs *flag.FlagSet, groups ...FlagGroup) *Flags {
	if len(groups) == 0 {
		return nil
	}
	f := &Flags{FlagSet: fs, groups: make(map[FlagGroup]interface{})}
	for _, g := range groups {
		switch g {
		case Runtime:
			f.groups[Runtime] = createAndRegisterRuntimeFlags(fs)
		case Listen:
			f.groups[Listen] = createAndRegisterListenFlags(fs)
		}
	}
	return f
}

// RuntimeFlags returns the Runtime flag subset stored in its Flags
// instance.
func (f *Flags) RuntimeFlags() RuntimeFlags {
	if p := f.groups[Runtime]; p == nil {
		return RuntimeFlags{}
	}
	from := f.groups[Runtime].(*RuntimeFlags)
	to := *from
	to.NamespaceRoots = make([]string, len(from.NamespaceRoots))
	copy(to.NamespaceRoots, from.NamespaceRoots)
	return to
}

// ListenFlags returns a copy of the Listen flag group stored in Flags.
// This copy will contain default values if the Listen flag group
// was not specified when CreateAndRegister was called. The HasGroup
// method can be used for testing to see if any given group was configured.
func (f *Flags) ListenFlags() ListenFlags {
	if p := f.groups[Listen]; p != nil {
		return *(p.(*ListenFlags))
	}
	return ListenFlags{}
}

// HasGroup returns group if the supplied FlagGroup has been created
// for these Flags.
func (f *Flags) HasGroup(group FlagGroup) bool {
	_, present := f.groups[group]
	return present
}

// Args returns the unparsed args, as per flag.Args.
func (f *Flags) Args() []string {
	return f.FlagSet.Args()
}

// readEnv reads the legacy NAMESPACE_ROOT? and VEYRON_CREDENTIALS env vars.
func readEnv() ([]string, string) {
	roots := []string{}
	for _, ev := range os.Environ() {
		p := strings.SplitN(ev, "=", 2)
		if len(p) != 2 {
			continue
		}
		k, v := p[0], p[1]
		if strings.HasPrefix(k, consts.NamespaceRootPrefix) && len(v) > 0 {
			roots = append(roots, v)
		}
	}
	return roots, os.Getenv(consts.VeyronCredentials)
}

// Parse parses the supplied args, as per flag.Parse
func (f *Flags) Parse(args []string) error {
	// TODO(cnicolaou): implement a single env var 'VANADIUM_OPTS'
	// that can be used to specify any command line.
	if err := f.FlagSet.Parse(args); err != nil {
		return err
	}

	hasrt := f.groups[Runtime] != nil
	if hasrt {
		runtime := f.groups[Runtime].(*RuntimeFlags)
		if runtime.namespaceRootsFlag.isSet {
			// command line overrides the environment.
			runtime.NamespaceRoots = runtime.namespaceRootsFlag.roots
		} else {
			// we have a default value for the command line, which
			// is only used if the environment variables have not been
			// supplied.
			if len(runtime.NamespaceRoots) == 0 {
				runtime.NamespaceRoots = runtime.namespaceRootsFlag.roots
			}
		}
	}
	return nil
}
