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
	// --veyron.acl (which may be repeated to supply multiple values)
	// ACL files are named - i.e. --veyron.acl=<name>:<file> with the
	// name <runtime> reserved for use by the runtime.
	ACL
)

const defaultNamespaceRoot = "/ns.dev.v.io:8101"

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
		// override the default value
		nsr.isSet = true
		nsr.roots = []string{}
	}
	nsr.roots = append(nsr.roots, v)
	return nil
}

type aclFlagVar struct {
	isSet bool
	files map[string]string
}

func (aclf *aclFlagVar) String() string {
	return fmt.Sprintf("%v", aclf.files)
}

func (aclf *aclFlagVar) Set(v string) error {
	if !aclf.isSet {
		// override the default value
		aclf.isSet = true
		aclf.files = make(map[string]string)
	}
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("%q is not in 'name:file' format", v)
	}
	name, file := parts[0], parts[1]
	aclf.files[name] = file
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

	// Vtrace flags control various aspects of Vtrace.
	Vtrace VtraceFlags

	namespaceRootsFlag namespaceRootFlagVar
}

type VtraceFlags struct {
	// VtraceSampleRate is the rate (from 0.0 - 1.0) at which
	// vtrace traces started by this process are sampled for collection.
	SampleRate float64

	// VtraceDumpOnShutdown tells the runtime to dump all stored traces
	// to Stderr at shutdown if true.
	DumpOnShutdown bool

	// VtraceCacheSize the number of traces to cache in memory.
	CacheSize int
}

// ACLFlags contains the values of the ACLFlags flag group.
type ACLFlags struct {
	flag aclFlagVar
}

// ACLFile returns the file which is presumed to contain ACL information
// associated with the supplied name parameter.
func (af ACLFlags) ACLFile(name string) string {
	return af.flag.files[name]
}

// ListenAddrs is the set of listen addresses captured from the command line.
// ListenAddrs mirrors ipc.ListenAddrs.
type ListenAddrs []struct {
	Protocol, Address string
}

// ListenFlags contains the values of the Listen flag group.
type ListenFlags struct {
	Addrs       ListenAddrs
	ListenProxy string
	protocol    TCPProtocolFlag
	addresses   ipHostPortFlagVar
}

type ipHostPortFlagVar struct {
	validator IPHostPortFlag
	flags     *ListenFlags
}

// Implements flag.Value.Get
func (ip ipHostPortFlagVar) Get() interface{} {
	return ip.String()
}

// Implements flag.Value.Set
func (ip *ipHostPortFlagVar) Set(s string) error {
	if err := ip.validator.Set(s); err != nil {
		return err
	}
	a := struct {
		Protocol, Address string
	}{
		ip.flags.protocol.String(),
		ip.validator.String(),
	}
	ip.flags.Addrs = append(ip.flags.Addrs, a)
	return nil
}

// Implements flag.Value.String
func (ip ipHostPortFlagVar) String() string {
	s := ""
	for _, a := range ip.flags.Addrs {
		s += fmt.Sprintf("(%s %s)", a.Protocol, a.Address)
	}
	return s
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

	fs.Float64Var(&f.Vtrace.SampleRate, "veyron.vtrace.sample_rate", 0.0, "Rate (from 0.0 to 1.0) to sample vtrace traces.")
	fs.BoolVar(&f.Vtrace.DumpOnShutdown, "veyron.vtrace.dump_on_shutdown", false, "If true, dump all stored traces on runtime shutdown.")
	fs.IntVar(&f.Vtrace.CacheSize, "veyron.vtrace.cache_size", 1024, "The number of vtrace traces to store in memory.")

	return f
}

func createAndRegisterACLFlags(fs *flag.FlagSet) *ACLFlags {
	f := &ACLFlags{}
	fs.Var(&f.flag, "veyron.acl", "specify an acl file as <name>:<aclfile>")
	return f
}

// createAndRegisterListenFlags creates and registers the ListenFlags
// group with the supplied flag.FlagSet.
func createAndRegisterListenFlags(fs *flag.FlagSet) *ListenFlags {
	f := &ListenFlags{
		protocol:  TCPProtocolFlag{"tcp"},
		addresses: ipHostPortFlagVar{validator: IPHostPortFlag{Port: "0"}},
	}
	f.addresses.flags = f

	fs.Var(&f.protocol, "veyron.tcp.protocol", "protocol to listen with")
	fs.Var(&f.addresses, "veyron.tcp.address", "address to listen on")
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
		case ACL:
			f.groups[ACL] = createAndRegisterACLFlags(fs)
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
		lf := p.(*ListenFlags)
		n := *lf
		if len(lf.Addrs) == 0 {
			n.Addrs = ListenAddrs{{n.protocol.String(),
				n.addresses.validator.String()}}
			return n
		}
		n.Addrs = make(ListenAddrs, len(lf.Addrs))
		copy(n.Addrs, lf.Addrs)
		return n
	}
	return ListenFlags{}
}

// ACLFlags returns a copy of the ACL flag group stored in Flags.
// This copy will contain default values if the ACL flag group
// was not specified when CreateAndRegister was called. The HasGroup
// method can be used for testing to see if any given group was configured.
func (f *Flags) ACLFlags() ACLFlags {
	if p := f.groups[ACL]; p != nil {
		return *(p.(*ACLFlags))
	}
	return ACLFlags{}
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

// Parse parses the supplied args, as per flag.Parse.
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
