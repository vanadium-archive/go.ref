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
	"v.io/x/lib/metadata"
)

const (
	separator          = "@"
	suffix             = "@@"
	blessingsSeparator = ","
	routeSeparator     = ","
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
	Protocol     string
	Address      string
	RID          naming.RoutingID
	RouteList    []string
	Blessings    []string
	IsMountTable bool
	IsLeaf       bool
}

// NewEndpoint creates a new endpoint from a string as per naming.NewEndpoint
func NewEndpoint(input string) (*Endpoint, error) {
	ep := new(Endpoint)

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
	case 6:
		err = ep.parseV6(parts)
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

func (ep *Endpoint) parseV6(parts []string) error {
	if len(parts) < 6 {
		return errInvalidEndpointString
	}

	ep.Protocol = parts[1]
	if len(ep.Protocol) == 0 {
		ep.Protocol = naming.UnknownProtocol
	}

	var ok bool
	if ep.Address, ok = naming.Unescape(parts[2]); !ok {
		return fmt.Errorf("invalid address: bad escape %s", parts[2])
	}
	if len(ep.Address) == 0 {
		ep.Address = net.JoinHostPort("", "0")
	}

	if len(parts[3]) > 0 {
		ep.RouteList = strings.Split(parts[3], routeSeparator)
		for i := range ep.RouteList {
			if ep.RouteList[i], ok = naming.Unescape(ep.RouteList[i]); !ok {
				return fmt.Errorf("invalid route: bad escape %s", ep.RouteList[i])
			}
		}
	}

	if err := ep.RID.FromString(parts[4]); err != nil {
		return fmt.Errorf("invalid routing id: %v", err)
	}

	var err error
	if ep.IsMountTable, ep.IsLeaf, err = parseMountTableFlag(parts[5]); err != nil {
		return fmt.Errorf("invalid mount table flag: %v", err)
	}
	// Join the remaining and re-split.
	if str := strings.Join(parts[6:], separator); len(str) > 0 {
		ep.Blessings = strings.Split(str, blessingsSeparator)
	}
	return nil
}

func (ep *Endpoint) RoutingID() naming.RoutingID {
	//nologcall
	return ep.RID
}

func (ep *Endpoint) Routes() []string {
	//nologcall
	return ep.RouteList
}

func (ep *Endpoint) Network() string {
	//nologcall
	return Network
}

func init() {
	metadata.Insert("v23.RPCEndpointVersion", fmt.Sprint(defaultVersion))
}

var defaultVersion = 6

func (ep *Endpoint) VersionedString(version int) string {
	// nologcall
	switch version {
	case 6:
		mt := "s"
		switch {
		case ep.IsLeaf:
			mt = "l"
		case ep.IsMountTable:
			mt = "m"
		}
		blessings := strings.Join(ep.Blessings, blessingsSeparator)
		escaped := make([]string, len(ep.RouteList))
		for i := range ep.RouteList {
			escaped[i] = naming.Escape(ep.RouteList[i], routeSeparator)
		}
		routes := strings.Join(escaped, routeSeparator)
		return fmt.Sprintf("@6@%s@%s@%s@%s@%s@%s@@",
			ep.Protocol, naming.Escape(ep.Address, "@"), routes, ep.RID, mt, blessings)
	default:
		return ep.VersionedString(defaultVersion)
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

func (ep *Endpoint) ServesLeaf() bool {
	//nologcall
	return ep.IsLeaf
}

func (ep *Endpoint) BlessingNames() []string {
	//nologcall
	return ep.Blessings
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
