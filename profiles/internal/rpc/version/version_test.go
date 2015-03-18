package version

import (
	"testing"

	inaming "v.io/x/ref/profiles/internal/naming"

	"v.io/v23/naming"
	"v.io/v23/rpc/version"
)

func TestCommonVersion(t *testing.T) {
	r := &Range{Min: 1, Max: 3}

	type testCase struct {
		localMin, localMax   version.RPCVersion
		remoteMin, remoteMax version.RPCVersion
		expectedVer          version.RPCVersion
		expectedErr          error
	}
	tests := []testCase{
		{0, 0, 0, 0, 0, UnknownVersionErr},
		{0, 1, 2, 3, 0, NoCompatibleVersionErr},
		{2, 3, 0, 1, 0, NoCompatibleVersionErr},
		{0, 5, 5, 6, 0, NoCompatibleVersionErr},
		{0, 2, 2, 4, 2, nil},
		{0, 2, 1, 3, 2, nil},
		{1, 3, 1, 3, 3, nil},
		{3, 3, 3, 3, 3, nil},
	}
	for _, tc := range tests {
		local := &inaming.Endpoint{
			MinRPCVersion: tc.localMin,
			MaxRPCVersion: tc.localMax,
		}
		remote := &inaming.Endpoint{
			MinRPCVersion: tc.remoteMin,
			MaxRPCVersion: tc.remoteMax,
		}
		if ver, err := r.CommonVersion(local, remote); ver != tc.expectedVer || err != tc.expectedErr {
			t.Errorf("Unexpected result for local: %v, remote: %v.  Got (%d, %v) wanted (%d, %v)",
				local, remote, ver, err, tc.expectedVer, tc.expectedErr)
		}
	}
}

func TestProxiedEndpoint(t *testing.T) {
	type testCase struct {
		supportMin, supportMax version.RPCVersion
		proxyMin, proxyMax     version.RPCVersion
		outMin, outMax         version.RPCVersion
		expectError            bool
	}
	tests := []testCase{
		{1, 3, 1, 2, 1, 2, false},
		{1, 3, 3, 5, 3, 3, false},
		{1, 3, 0, 1, 1, 1, false},
		{1, 3, 0, 1, 1, 1, false},
		{0, 0, 0, 0, 0, 0, true},
		{2, 5, 0, 1, 0, 0, true},
		{2, 5, 6, 7, 0, 0, true},
	}

	rid := naming.FixedRoutingID(1)
	for _, tc := range tests {
		r := &Range{Min: tc.supportMin, Max: tc.supportMax}
		proxy := &inaming.Endpoint{
			MinRPCVersion: tc.proxyMin,
			MaxRPCVersion: tc.proxyMax,
		}
		if ep, err := r.ProxiedEndpoint(rid, proxy); err != nil {
			if !tc.expectError {
				t.Errorf("Unexpected error for case %+v: %v", tc, err)
			}
		} else {
			if tc.expectError {
				t.Errorf("Expected Error, but got result for test case %+v", tc)
				continue
			}
			if ep.MinRPCVersion != tc.outMin || ep.MaxRPCVersion != tc.outMax {
				t.Errorf("Unexpected range for case %+v.  Got (%d, %d) want (%d, %d)",
					tc, ep.MinRPCVersion, ep.MaxRPCVersion, tc.outMin, tc.outMax)
			}
		}
	}
}

func TestCheckCompatibility(t *testing.T) {
	type testCase struct {
		supportMin, supportMax version.RPCVersion
		remoteMin, remoteMax   version.RPCVersion
		expectedError          error
	}
	tests := []testCase{
		{0, 0, 0, 0, UnknownVersionErr},
		{5, 10, 1, 4, NoCompatibleVersionErr},
		{1, 4, 5, 10, NoCompatibleVersionErr},
		{1, 10, 2, 9, nil},
		{3, 8, 1, 4, nil},
		{3, 8, 7, 9, nil},
	}

	for _, tc := range tests {
		r := &Range{Min: tc.supportMin, Max: tc.supportMax}
		remote := &inaming.Endpoint{
			MinRPCVersion: tc.remoteMin,
			MaxRPCVersion: tc.remoteMax,
		}
		if err := r.CheckCompatibility(remote); err != tc.expectedError {
			t.Errorf("Unexpected error for case %+v: got %v, wanted %v",
				tc, err, tc.expectedError)
		}
	}
}
