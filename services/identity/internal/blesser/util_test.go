package blesser

import (
	"v.io/v23/rpc"
	"v.io/v23/security"
)

type serverCall struct {
	rpc.StreamServerCall
	method        string
	p             security.Principal
	local, remote security.Blessings
}

func (c *serverCall) Method() string                      { return c.method }
func (c *serverCall) LocalPrincipal() security.Principal  { return c.p }
func (c *serverCall) LocalBlessings() security.Blessings  { return c.local }
func (c *serverCall) RemoteBlessings() security.Blessings { return c.remote }

func blessSelf(p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return b
}

func newCaveat(c security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return c
}
