package flags

import "flag"

type Flags struct {
	// The FlagSet passed in as a parameter to New
	FlagSet *flag.FlagSet

	// As needed to initialize ipc.ListenSpec.
	ListenProtocolFlag TCPProtocolFlag
	ListenAddressFlag  IPHostPortFlag
	ListenProxyFlag    string

	// TODO(cnicolaou): implement these.
	NamespaceRootsFlag string // TODO(cnicolaou): provide flag.Value impl
	CredentialsFlag    string // TODO(cnicolaou): provide flag.Value impl
}

// New returns a new instance of flags.Flags. Calling Parse on the supplied
// flagSet will populate the fields of the returned flags.Flags with the
// appropriate values. The currently supported set of flags only allows
// for configuring one service at a time, but in the future this will
// be expanded to support multiple services.
func New(fs *flag.FlagSet) *Flags {
	t := &Flags{FlagSet: fs}
	t.ListenProtocolFlag = TCPProtocolFlag{"tcp"}
	t.ListenAddressFlag = IPHostPortFlag{Port: "0"}
	fs.Var(&t.ListenProtocolFlag, "veyron.tcp.protocol", "protocol to listen with")
	fs.Var(&t.ListenAddressFlag, "veyron.tcp.address", "address to listen on")
	fs.StringVar(&t.ListenProxyFlag, "veyron.proxy", "", "object name of proxy service to use to export services across network boundaries")
	fs.StringVar(&t.NamespaceRootsFlag, "veyron.namespace.roots", "", "colon separated list of roots for the local namespace")
	fs.StringVar(&t.CredentialsFlag, "veyron.credentials", "", "directory to use for storing security credentials")
	return t
}

// Args returns the unparsed args, as per flag.Args
func (fs *Flags) Args() []string {
	return fs.FlagSet.Args()
}

// Parse parses the supplied args, as per flag.Parse
func (fs *Flags) Parse(args []string) error {
	return fs.FlagSet.Parse(args)
}
