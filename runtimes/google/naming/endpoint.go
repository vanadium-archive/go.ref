package naming

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"veyron.io/veyron/veyron2/ipc/version"
	"veyron.io/veyron/veyron2/naming"
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
}

// NewEndpoint creates a new endpoint from a string as per naming.NewEndpoint
func NewEndpoint(input string) (*Endpoint, error) {
	var ep Endpoint

	// The prefix and suffix are optional.
	input = strings.TrimPrefix(strings.TrimSuffix(input, suffix), separator)

	parts := strings.Split(input, separator)
	if len(parts) == 1 {
		err := ep.parseHostPort(parts[0])
		return &ep, err
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
	default:
		err = errInvalidEndpointString
	}
	return &ep, err
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

func (ep *Endpoint) RoutingID() naming.RoutingID {
	//nologcall
	return ep.RID
}
func (ep *Endpoint) Network() string {
	//nologcall
	return Network
}
func (ep *Endpoint) String() string {
	//nologcall
	return fmt.Sprintf("%s2@%s@%s@%s@%s@%s@@",
		separator, ep.Protocol, ep.Address, ep.RID,
		printIPCVersion(ep.MinIPCVersion), printIPCVersion(ep.MaxIPCVersion))
}
func (ep *Endpoint) version() int { return 2 }

func (ep *Endpoint) Addr() net.Addr {
	//nologcall
	return &addr{network: ep.Protocol, address: ep.Address}
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
