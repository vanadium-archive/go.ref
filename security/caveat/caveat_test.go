package caveat_test

import (
	"net"
	"testing"
	"time"

	"veyron/security/caveat"
	"veyron2/naming"
	"veyron2/security"
)

// endpoint implements naming.Endpoint
type endpoint struct {
	naming.Endpoint
	addr net.Addr
}

func (e endpoint) Addr() net.Addr { return e.addr }

type context struct {
	local, remote                 security.PublicID
	localEndpoint, remoteEndpoint endpoint
	method                        string
}

func (c *context) Method() string                            { return c.method }
func (c *context) Name() string                              { return "some_name" }
func (c *context) Suffix() string                            { return "some_suffix" }
func (c *context) Label() security.Label                     { return security.AdminLabel }
func (c *context) Discharges() map[string]security.Discharge { return nil }
func (c *context) LocalID() security.PublicID                { return c.local }
func (c *context) RemoteID() security.PublicID               { return c.remote }
func (c *context) LocalEndpoint() naming.Endpoint            { return &c.localEndpoint }
func (c *context) RemoteEndpoint() naming.Endpoint           { return &c.remoteEndpoint }

func TestCaveats(t *testing.T) {
	var (
		alice = security.FakePublicID("alice")
		bob   = security.FakePublicID("bob")
	)
	now := time.Now()
	tests := []struct {
		c  security.CaveatValidator
		ok bool
	}{
		{&caveat.Expiry{IssueTime: now, ExpiryTime: now.Add(time.Hour)}, true},
		{&caveat.Expiry{IssueTime: now.Add(-1 * time.Hour), ExpiryTime: now.Add(-1 * time.Minute)}, false},
		{caveat.MethodRestriction(nil), false},
		{caveat.NetworkType("udp"), false},
		{caveat.NetworkType("tcp"), true},
		{caveat.MethodRestriction{"Pause", "Play"}, true},
		{caveat.MethodRestriction{"List"}, false},
		{caveat.PeerBlessings(nil), false},
		{caveat.PeerBlessings{"fake/alice"}, true},
		{caveat.PeerBlessings{"fake/carol"}, false},
		{caveat.PeerBlessings{"fake/alice", "fake/carol"}, true},
	}
	ctx := &context{local: alice, remote: bob, method: "Play", remoteEndpoint: endpoint{addr: &net.TCPAddr{}}}
	for _, test := range tests {
		if err := test.c.Validate(ctx); test.ok != (err == nil) {
			t.Errorf("Caveat:%#v. Got error:%v, want error:%v", test.c, err, test.ok)
		}
	}
}
