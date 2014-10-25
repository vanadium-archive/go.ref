// Package flags provides flag definitions for commonly used flags and
// and, where appropriate, implementations of the flag.Value interface
// for those flags to ensure that only valid values of those flags
// are supplied.
// In general, this package will be used by veyron profiles and the
// runtime implementations, but can also be used by any application
// that wants access to the flags and environment variables it supports.
//
// TODO(cnicolaou): move reading of environment variables to here also,
// flags will override the environment variable settings.
package flags

import "flag"

type Flags struct {
	ListenProtocolFlag TCPProtocolFlag
	ListenAddressFlag  IPHostPortFlag
	ListenProxyFlag    string
	NamespaceRootsFlag string // TODO(cnicolaou): provide flag.Value impl
	CredentialsFlag    string // TODO(cnicolaou): provide flag.Value impl
}

// New returns a new instance of flags.Flags. Calling Parse on the supplied
// flagSet will populate the fields of the return flags.Flags with the appropriate
// values.
func New(fs *flag.FlagSet) *Flags {
	t := &Flags{}
	t.ListenProtocolFlag = TCPProtocolFlag{"tcp"}
	t.ListenAddressFlag = IPHostPortFlag{Port: "0"}
	fs.Var(&t.ListenProtocolFlag, "veyron.tcp.protocol", "protocol to listen with")
	fs.Var(&t.ListenAddressFlag, "veyron.tcp.address", "address to listen on")
	fs.StringVar(&t.ListenProxyFlag, "veyron.proxy", "", "object name of proxy service to use to export services across network boundaries")
	fs.StringVar(&t.NamespaceRootsFlag, "veyron.namespace.roots", "", ": separated list of roots for the local namespace")
	fs.StringVar(&t.CredentialsFlag, "veyron.credentials", "", "directory to use for storing security credentials")
	return t
}
