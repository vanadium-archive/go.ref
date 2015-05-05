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
	Blessings    []string
	IsMountTable bool
	IsLeaf       bool
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

func (ep *Endpoint) parseV5(parts []string) error {
	if len(parts) < 5 {
		return errInvalidEndpointString
	}

	ep.Protocol = parts[1]
	if len(ep.Protocol) == 0 {
		ep.Protocol = naming.UnknownProtocol
	}

	var err error
	if ep.Address, err = unescape(parts[2]); err != nil {
		return fmt.Errorf("invalid address: %v", err)
	}
	if len(ep.Address) == 0 {
		ep.Address = net.JoinHostPort("", "0")
	}

	if err := ep.RID.FromString(parts[3]); err != nil {
		return fmt.Errorf("invalid routing id: %v", err)
	}

	if ep.IsMountTable, ep.IsLeaf, err = parseMountTableFlag(parts[4]); err != nil {
		return fmt.Errorf("invalid mount table flag: %v", err)
	}
	// Join the remaining and re-split.
	if str := strings.Join(parts[5:], separator); len(str) > 0 {
		ep.Blessings = strings.Split(str, blessingsSeparator)
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

func init() {
	metadata.Insert("v23.RPCEndpointVersion", fmt.Sprint(defaultVersion))
}

var defaultVersion = 5

func (ep *Endpoint) VersionedString(version int) string {
	switch version {
	default:
		return ep.VersionedString(defaultVersion)
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
			ep.Protocol, escape(ep.Address), ep.RID, mt, blessings)
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

func escape(s string) string {
	if !strings.ContainsAny(s, "%@") {
		return s
	}
	t := make([]byte, len(s)*3)
	j := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '@', '%':
			t[j] = '%'
			t[j+1] = "0123456789ABCDEF"[c>>4]
			t[j+2] = "0123456789ABCDEF"[c&15]
			j += 3
		default:
			t[j] = c
			j++
		}
	}
	return string(t[:j])
}

func ishex(c byte) bool {
	switch {
	case '0' <= c && c <= '9':
		return true
	case 'a' <= c && c <= 'f':
		return true
	case 'A' <= c && c <= 'F':
		return true
	}
	return false
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func unescape(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	t := make([]byte, len(s))
	j := 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '%':
			if len(s) <= i+2 || !ishex(s[i+1]) || !ishex(s[i+2]) {
				return s, fmt.Errorf("invalid escape %q", s)
			}
			t[j] = unhex(s[i+1])<<4 | unhex(s[i+2])
			j++
			i += 3
		default:
			t[j] = s[i]
			j++
			i++
		}
	}
	return string(t[:j]), nil
}
