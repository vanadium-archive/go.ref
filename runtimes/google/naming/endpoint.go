package naming

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"v.io/core/veyron2/ipc/version"
	"v.io/core/veyron2/naming"
)

const (
	separator = "@"
	suffix    = "@@"
)

var errInvalidEndpointString = errors.New("invalid endpoint string")

// Network is the string returned by naming.Endpoint.Network implementations
// defined in this package.
const Network = "veyron"

// Endpoint is a naming.Endpoint implementation used to convey RPC information.
type Endpoint struct {
	Protocol      string
	Address       string
	RID           naming.RoutingID
	MinIPCVersion version.IPCVersion
	MaxIPCVersion version.IPCVersion
	IsMountTable  bool
}

// NewEndpoint creates a new endpoint from a string as per naming.NewEndpoint
func NewEndpoint(input string) (*Endpoint, error) {
	ep := new(Endpoint)

	// We have to guess this is a mount table if we don't know.
	ep.IsMountTable = true

	// The prefix and suffix are optional.
	input = strings.TrimPrefix(strings.TrimSuffix(input, suffix), separator)

	parts := strings.Split(input, separator)
	if len(parts) == 1 {
		err := ep.parseHostPort(parts[0])
		return ep, err
	}

	version, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid version: %v", err)
	}

	switch version {
	case 1:
		err = ep.parseV1(parts)
	case 2:
		err = ep.parseV2(parts)
	case 3:
		err = ep.parseV3(parts)
	default:
		err = errInvalidEndpointString
	}
	return ep, err
}

func (ep *Endpoint) parseHostPort(input string) error {
	// Could be in host:port format.
	if _, _, err := net.SplitHostPort(input); err != nil {
		return errInvalidEndpointString
	}
	ep.Protocol = "tcp"
	ep.Address = input
	ep.RID = naming.NullRoutingID

	return nil
}

func (ep *Endpoint) parseV1(parts []string) error {
	if len(parts) != 4 {
		return errInvalidEndpointString
	}

	ep.Protocol = parts[1]
	if len(ep.Protocol) == 0 {
		ep.Protocol = "tcp"
	}

	ep.Address = parts[2]
	if len(ep.Address) == 0 {
		ep.Address = net.JoinHostPort("", "0")
	}

	if err := ep.RID.FromString(parts[3]); err != nil {
		return fmt.Errorf("invalid routing id: %v", err)
	}

	return nil
}

func parseIPCVersion(input string) (version.IPCVersion, error) {
	if input == "" {
		return version.UnknownIPCVersion, nil
	}
	v, err := strconv.ParseUint(input, 10, 32)
	if err != nil {
		err = fmt.Errorf("invalid IPC version: %s, %v", err)
	}
	return version.IPCVersion(v), err
}

func printIPCVersion(v version.IPCVersion) string {
	if v == version.UnknownIPCVersion {
		return ""
	}
	return strconv.FormatUint(uint64(v), 10)
}

func parseMountTableFlag(input string) (bool, error) {
	switch len(input) {
	case 0:
		return true, nil
	case 1:
		switch f := input[0]; f {
		case 'm':
			return true, nil
		case 's':
			return false, nil
		default:
			return false, fmt.Errorf("%c is not one of 'm' or 's'", f)
		}
	}
	return false, fmt.Errorf("flag is either missing or too long")
}

func (ep *Endpoint) parseV2(parts []string) error {
	var err error
	if len(parts) != 6 {
		return errInvalidEndpointString
	}
	if err = ep.parseV1(parts[:4]); err != nil {
		return err
	}
	if ep.MinIPCVersion, err = parseIPCVersion(parts[4]); err != nil {
		return fmt.Errorf("invalid IPC version: %v", err)
	}
	if ep.MaxIPCVersion, err = parseIPCVersion(parts[5]); err != nil {
		return fmt.Errorf("invalid IPC version: %v", err)
	}
	return nil
}

func (ep *Endpoint) parseV3(parts []string) error {
	var err error
	if len(parts) != 7 {
		return errInvalidEndpointString
	}
	if err = ep.parseV2(parts[:6]); err != nil {
		return err
	}
	if ep.IsMountTable, err = parseMountTableFlag(parts[6]); err != nil {
		return fmt.Errorf("invalid mount table flag: %v", err)
	}
	return nil
}

func (ep *Endpoint) RoutingID() naming.RoutingID {
	//nologcall
	return ep.RID
}
func (ep *Endpoint) Network() string {
	//nologcall
	return Network
}

var defaultVersion = 3

func (ep *Endpoint) VersionedString(version int) string {
	switch version {
	default:
		return ep.VersionedString(defaultVersion)
	case 1:
		return fmt.Sprintf("@1@%s@%s@@", ep.Protocol, ep.Address)
	case 2:
		return fmt.Sprintf("@2@%s@%s@%s@%s@%s@@",
			ep.Protocol, ep.Address, ep.RID,
			printIPCVersion(ep.MinIPCVersion), printIPCVersion(ep.MaxIPCVersion))
	case 3:
		mt := "s"
		if ep.IsMountTable {
			mt = "m"
		}
		return fmt.Sprintf("@3@%s@%s@%s@%s@%s@%s@@",
			ep.Protocol, ep.Address, ep.RID,
			printIPCVersion(ep.MinIPCVersion), printIPCVersion(ep.MaxIPCVersion),
			mt)
	}
}

func (ep *Endpoint) String() string {
	//nologcall
	return ep.VersionedString(defaultVersion)
}

func (ep *Endpoint) Name() string {
	//nologcall
	return naming.JoinAddressName(ep.String(), "")
}

func (ep *Endpoint) Addr() net.Addr {
	//nologcall
	return &addr{network: ep.Protocol, address: ep.Address}
}

func (ep *Endpoint) ServesMountTable() bool {
	//nologcall
	return ep.IsMountTable
}

type addr struct {
	network, address string
}

func (a *addr) Network() string {
	return a.network
}

func (a *addr) String() string {
	return a.address
}
