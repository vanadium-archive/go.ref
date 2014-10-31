package principal

import (
	"fmt"
	"strings"

	vsecurity "veyron.io/veyron/veyron/security"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
)

type context struct {
	method string
	local  security.Principal
}

func (c *context) Method() string                            { return c.method }
func (c *context) Name() string                              { return "" }
func (c *context) Suffix() string                            { return "" }
func (c *context) Label() security.Label                     { return 0 }
func (c *context) Discharges() map[string]security.Discharge { return nil }
func (c *context) LocalPrincipal() security.Principal        { return c.local }
func (c *context) LocalBlessings() security.Blessings        { return nil }
func (c *context) RemoteBlessings() security.Blessings       { return nil }
func (c *context) LocalEndpoint() naming.Endpoint            { return nil }
func (c *context) RemoteEndpoint() naming.Endpoint           { return nil }

func newPrincipal() security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	return p
}

func blessSelf(p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return b
}

func matchesError(got error, want string) error {
	if (got == nil) && len(want) == 0 {
		return nil
	}
	if got == nil {
		return fmt.Errorf("Got nil error, wanted to match %q", want)
	}
	if !strings.Contains(got.Error(), want) {
		return fmt.Errorf("Got error %q, wanted to match %q", got, want)
	}
	return nil
}
