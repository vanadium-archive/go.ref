package lib

import (
	"fmt"
	"regexp"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/naming"
)

// Turns a list of names into a list of names that use the "ws" protocol.
func EndpointsToWs(rt veyron2.Runtime, names []string) ([]string, error) {
	outNames := []string{}
	tcpRegexp := regexp.MustCompile(`@tcp\d*@`)
	for _, name := range names {
		addr, suff := naming.SplitAddressName(name)
		ep, err := rt.NewEndpoint(addr)
		if err != nil {
			return nil, fmt.Errorf("rt.NewEndpoint(%v) failed: %v", addr, err)
		}
		// Replace only the first match.
		first := true
		wsEp := tcpRegexp.ReplaceAllFunc([]byte(ep.String()), func(s []byte) []byte {
			if first {
				first = false
				return []byte("@ws@")
			}
			return s
		})
		wsName := naming.JoinAddressName(string(wsEp), suff)

		outNames = append(outNames, wsName)
	}
	return outNames, nil
}
