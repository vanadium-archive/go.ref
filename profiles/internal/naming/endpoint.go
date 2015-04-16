// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package naming

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"v.io/v23/naming"
	"v.io/v23/rpc/version"
)

const (
	separator          = "@"
	suffix             = "@@"
	blessingsSeparator = ","
)

var (
	errInvalidEndpointString = errors.New("invalid endpoint string")
	hostportEP               = regexp.MustCompile("^(?:\\((.*)\\)@)?([^@]+)$")
)

// Network is the string returned by naming.Endpoint.Network implementations
// defined in this package.
const Network = "v23"

// Endpoint is a naming.Endpoint implementation used to convey RPC information.
type Endpoint struct {
	Protocol      string
	Address       string
	RID           naming.RoutingID
	MinRPCVersion version.RPCVersion
	MaxRPCVersion version.RPCVersion
	Blessings     []string
	IsMountTable  bool
	IsLeaf        bool
}

// NewEndpoint creates a new endpoint from a string as per naming.NewEndpoint
func NewEndpoint(input string) (*Endpoint, error) {
	ep := new(Endpoint)

	// We have to guess this is a mount table if we don't know.
	ep.IsMountTable = true

	// If the endpoint does not end in a @, it must be in [blessing@]host:port format.
	if parts := hostportEP.FindStringSubmatch(input); len(parts) > 0 {
		hostport := parts[len(parts)-1]
		var blessing string
		if len(parts) > 2 {
			blessing = parts[1]
		}
		err := ep.parseHostPort(blessing, hostport)
		return ep, err
	}
	// Trim the prefix and suffix and parse the rest.
	input = strings.TrimPrefix(strings.TrimSuffix(input, suffix), separator)
	parts := strings.Split(input, separator)
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
	case 4:
		err = ep.parseV4(parts)
	case 5:
		err = ep.parseV5(parts)
	default:
		err = errInvalidEndpointString
	}
	return ep, err
}

func (ep *Endpoint) parseHostPort(blessing, hostport string) error {
	// Could be in host:port format.
	if _, _, err := net.SplitHostPort(hostport); err != nil {
		return errInvalidEndpointString
	}
	ep.Protocol = naming.UnknownProtocol
	ep.Address = hostport
	ep.RID = naming.NullRoutingID
	if len(blessing) > 0 {
		ep.Blessings = []string{blessing}
	}
	return nil
}

func (ep *Endpoint) parseV1(parts []string) error {
	if len(parts) != 4 {
		return errInvalidEndpointString
	}

	ep.Protocol = parts[1]
	if len(ep.Protocol) == 0 {
		ep.Protocol = naming.UnknownProtocol
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

func parseRPCVersion(input string) (version.RPCVersion, error) {
	if input == "" {
		return version.UnknownRPCVersion, nil
	}
	v, err := strconv.ParseUint(input, 10, 32)
	if err != nil {
		err = fmt.Errorf("invalid RPC version: %s, %v", input, err)
	}
	return version.RPCVersion(v), err
}

func printRPCVersion(v version.RPCVersion) string {
	if v == version.UnknownRPCVersion {
		return ""
	}
	return strconv.FormatUint(uint64(v), 10)
}

func parseMountTableFlag(input string) (bool, bool, error) {
	switch len(input) {
	case 0:
		return true, false, nil
	case 1:
		switch f := input[0]; f {
		case 'l':
			return false, true, nil
		case 'm':
			return true, false, nil
		case 's':
			return false, false, nil
		default:
			return false, false, fmt.Errorf("%c is not one of 'l', 'm', or 's'", f)
		}
	}
	return false, false, fmt.Errorf("flag is either missing or too long")
}

func (ep *Endpoint) parseV2(parts []string) error {
	var err error
	if len(parts) != 6 {
		return errInvalidEndpointString
	}
	if err = ep.parseV1(parts[:4]); err != nil {
		return err
	}
	if ep.MinRPCVersion, err = parseRPCVersion(parts[4]); err != nil {
		return fmt.Errorf("invalid RPC version: %v", err)
	}
	if ep.MaxRPCVersion, err = parseRPCVersion(parts[5]); err != nil {
		return fmt.Errorf("invalid RPC version: %v", err)
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
	if ep.IsMountTable, ep.IsLeaf, err = parseMountTableFlag(parts[6]); err != nil {
		return fmt.Errorf("invalid mount table flag: %v", err)
	}
	return nil
}

func (ep *Endpoint) parseV4(parts []string) error {
	if len(parts) < 7 {
		return errInvalidEndpointString
	}
	if err := ep.parseV3(parts[:7]); err != nil {
		return err
	}
	// Join the remaining and re-split.
	if str := strings.Join(parts[7:], separator); len(str) > 0 {
		ep.Blessings = strings.Split(str, blessingsSeparator)
	}
	return nil
}

func (ep *Endpoint) parseV5(parts []string) error {
	if len(parts) < 5 {
		return errInvalidEndpointString
	}
	var err error
	if err = ep.parseV1(parts[:4]); err != nil {
		return err
	}
	if ep.IsMountTable, ep.IsLeaf, err = parseMountTableFlag(parts[4]); err != nil {
		return fmt.Errorf("invalid mount table flag: %v", err)
	}
	// Join the remaining and re-split.
	if str := strings.Join(parts[5:], separator); len(str) > 0 {
		ep.Blessings = strings.Split(str, blessingsSeparator)
	}
	ep.MinRPCVersion = version.DeprecatedRPCVersion
	ep.MaxRPCVersion = version.DeprecatedRPCVersion
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

var defaultVersion = 3 // TODO(ashankar): Change to 4?

func (ep *Endpoint) VersionedString(version int) string {
	switch version {
	default:
		return ep.VersionedString(defaultVersion)
	case 1:
		return fmt.Sprintf("@1@%s@%s@@", ep.Protocol, ep.Address)
	case 2:
		return fmt.Sprintf("@2@%s@%s@%s@%s@%s@@",
			ep.Protocol, ep.Address, ep.RID,
			printRPCVersion(ep.MinRPCVersion), printRPCVersion(ep.MaxRPCVersion))
	case 3:
		mt := "s"
		if ep.IsMountTable {
			mt = "m"
		}
		return fmt.Sprintf("@3@%s@%s@%s@%s@%s@%s@@",
			ep.Protocol, ep.Address, ep.RID,
			printRPCVersion(ep.MinRPCVersion), printRPCVersion(ep.MaxRPCVersion),
			mt)
	case 4:
		mt := "s"
		switch {
		case ep.IsLeaf:
			mt = "l"
		case ep.IsMountTable:
			mt = "m"
		}
		blessings := strings.Join(ep.Blessings, blessingsSeparator)
		return fmt.Sprintf("@4@%s@%s@%s@%s@%s@%s@%s@@",
			ep.Protocol, ep.Address, ep.RID,
			printRPCVersion(ep.MinRPCVersion), printRPCVersion(ep.MaxRPCVersion),
			mt, blessings)
	case 5:
		mt := "s"
		switch {
		case ep.IsLeaf:
			mt = "l"
		case ep.IsMountTable:
			mt = "m"
		}
		blessings := strings.Join(ep.Blessings, blessingsSeparator)
		return fmt.Sprintf("@5@%s@%s@%s@%s@%s@@",
			ep.Protocol, ep.Address, ep.RID, mt, blessings)
	}
}

func (ep *Endpoint) String() string {
	//nologcall
	// Use version 4 if blessings are present, otherwise there is a loss of information.
	v := defaultVersion
	if len(ep.Blessings) > 0 && v < 4 {
		v = 4
	}
	return ep.VersionedString(v)
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

func (ep *Endpoint) ServesLeaf() bool {
	//nologcall
	return ep.IsLeaf
}

func (ep *Endpoint) BlessingNames() []string {
	//nologcall
	return ep.Blessings
}

func (ep *Endpoint) RPCVersionRange() version.RPCVersionRange {
	//nologcall
	return version.RPCVersionRange{Min: ep.MinRPCVersion, Max: ep.MaxRPCVersion}
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
