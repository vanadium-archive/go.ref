package caveat_test

import (
	"testing"
	"time"

	"veyron/security/caveat"
	"veyron2/security"
)

type context struct {
	local, remote security.PublicID
	method        string
}

func (c *context) Method() string                                { return c.method }
func (c *context) Name() string                                  { return "some_name" }
func (c *context) Suffix() string                                { return "some_suffix" }
func (c *context) Label() security.Label                         { return security.AdminLabel }
func (c *context) CaveatDischarges() security.CaveatDischargeMap { return nil }
func (c *context) LocalID() security.PublicID                    { return c.local }
func (c *context) RemoteID() security.PublicID                   { return c.remote }

func TestCaveats(t *testing.T) {
	var (
		alice = security.FakePublicID("alice")
		bob   = security.FakePublicID("bob")
	)
	now := time.Now()
	tests := []struct {
		c  security.Caveat
		ok bool
	}{
		{&caveat.Expiry{IssueTime: now, ExpiryTime: now.Add(time.Hour)}, true},
		{&caveat.Expiry{IssueTime: now.Add(-1 * time.Hour), ExpiryTime: now.Add(-1 * time.Minute)}, false},
		{caveat.MethodRestriction(nil), false},
		{caveat.MethodRestriction{"Pause", "Play"}, true},
		{caveat.MethodRestriction{"List"}, false},
		{caveat.PeerIdentity(nil), false},
		{caveat.PeerIdentity{"fake/alice"}, true},
		{caveat.PeerIdentity{"fake/carol"}, false},
		{caveat.PeerIdentity{"fake/alice", "fake/carol"}, true},
	}
	ctx := &context{local: alice, remote: bob, method: "Play"}
	for _, test := range tests {
		if err := test.c.Validate(ctx); test.ok != (err == nil) {
			t.Errorf("Caveat:%#v. Got error:%v, want error:%v", test.c, err, test.ok)
		}
	}
}
