package ipc

import (
	"testing"
	"time"

	tsecurity "v.io/x/ref/lib/testutil/security"

	"v.io/v23/security"
	"v.io/v23/vdl"
)

func TestDischargeClientCache(t *testing.T) {
	dcc := &dischargeCache{
		cache:    make(map[dischargeCacheKey]security.Discharge),
		idToKeys: make(map[string][]dischargeCacheKey),
	}

	var (
		discharger = tsecurity.NewPrincipal("discharger")
		expiredCav = mkCaveat(security.NewPublicKeyCaveat(discharger.PublicKey(), "moline", security.ThirdPartyRequirements{}, security.UnconstrainedUse()))
		argsCav    = mkCaveat(security.NewPublicKeyCaveat(discharger.PublicKey(), "moline", security.ThirdPartyRequirements{}, security.UnconstrainedUse()))
		methodCav  = mkCaveat(security.NewPublicKeyCaveat(discharger.PublicKey(), "moline", security.ThirdPartyRequirements{}, security.UnconstrainedUse()))
		serverCav  = mkCaveat(security.NewPublicKeyCaveat(discharger.PublicKey(), "moline", security.ThirdPartyRequirements{}, security.UnconstrainedUse()))

		dExpired = mkDischarge(discharger.MintDischarge(expiredCav, mkCaveat(security.ExpiryCaveat(time.Now().Add(-1*time.Minute)))))
		dArgs    = mkDischarge(discharger.MintDischarge(argsCav, security.UnconstrainedUse()))
		dMethod  = mkDischarge(discharger.MintDischarge(methodCav, security.UnconstrainedUse()))
		dServer  = mkDischarge(discharger.MintDischarge(serverCav, security.UnconstrainedUse()))

		emptyImp       = security.DischargeImpetus{}
		argsImp        = security.DischargeImpetus{Arguments: []*vdl.Value{&vdl.Value{}}}
		methodImp      = security.DischargeImpetus{Method: "foo"}
		otherMethodImp = security.DischargeImpetus{Method: "bar"}
		serverImp      = security.DischargeImpetus{Server: []security.BlessingPattern{security.BlessingPattern("fooserver")}}
		otherServerImp = security.DischargeImpetus{Server: []security.BlessingPattern{security.BlessingPattern("barserver")}}
	)

	// Discharges for different cavs should not be cached.
	d := mkDischarge(discharger.MintDischarge(argsCav, security.UnconstrainedUse()))
	dcc.Add(d, emptyImp)
	outdis := make([]*security.Discharge, 1)
	if remaining := dcc.Discharges([]security.Caveat{methodCav}, []security.DischargeImpetus{emptyImp}, outdis); remaining == 0 {
		t.Errorf("Discharge for different caveat should not have been in cache")
	}
	dcc.invalidate(d)

	// Add some discharges into the cache.
	dcc.Add(dArgs, argsImp)
	dcc.Add(dMethod, methodImp)
	dcc.Add(dServer, serverImp)
	dcc.Add(dExpired, emptyImp)

	testCases := []struct {
		caveat          security.Caveat           // caveat that we are fetching discharges for.
		queryImpetus    security.DischargeImpetus // Impetus used to  query the cache.
		cachedDischarge *security.Discharge       // Discharge that we expect to be returned from the cache, nil if the discharge should not be cached.
	}{
		// Expired discharges should not be returned by the cache.
		{expiredCav, emptyImp, nil},

		// Discharges with Impetuses that have Arguments should not be cached.
		{argsCav, argsImp, nil},

		{methodCav, methodImp, &dMethod},
		{methodCav, otherMethodImp, nil},
		{methodCav, emptyImp, nil},

		{serverCav, serverImp, &dServer},
		{serverCav, otherServerImp, nil},
		{serverCav, emptyImp, nil},
	}

	for i, test := range testCases {
		out := make([]*security.Discharge, 1)
		remaining := dcc.Discharges([]security.Caveat{test.caveat}, []security.DischargeImpetus{test.queryImpetus}, out)
		if test.cachedDischarge != nil {
			got := "nil"
			if remaining == 0 {
				got = out[0].ID()
			}
			if got != test.cachedDischarge.ID() {
				t.Errorf("#%d: got discharge %v, want %v, queried with %v", i, got, test.cachedDischarge.ID(), test.queryImpetus)
			}
		} else if remaining == 0 {
			t.Errorf("#%d: discharge %v should not have been in cache, queried with %v", i, out[0].ID(), test.queryImpetus)
		}
	}
	if t.Failed() {
		t.Logf("dArgs.ID():    %v", dArgs.ID())
		t.Logf("dMethod.ID():  %v", dMethod.ID())
		t.Logf("dServer.ID():  %v", dServer.ID())
		t.Logf("dExpired.ID(): %v", dExpired.ID())
	}
}

func mkDischarge(d security.Discharge, err error) security.Discharge {
	if err != nil {
		panic(err)
	}
	return d
}
