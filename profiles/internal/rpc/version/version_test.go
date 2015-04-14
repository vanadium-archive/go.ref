// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package version

import (
	"testing"

	inaming "v.io/x/ref/profiles/internal/naming"

	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/verror"
)

func TestCommonVersion(t *testing.T) {
	r := &Range{Min: 1, Max: 3}

	type testCase struct {
		localMin, localMax   version.RPCVersion
		remoteMin, remoteMax version.RPCVersion
		expectedVer          version.RPCVersion
		expectedErr          verror.IDAction
	}
	tests := []testCase{
		{0, 0, 0, 0, 0, ErrUnknownVersion},
		{0, 1, 2, 3, 0, ErrNoCompatibleVersion},
		{2, 3, 0, 1, 0, ErrNoCompatibleVersion},
		{0, 5, 5, 6, 0, ErrNoCompatibleVersion},
		{0, 2, 2, 4, 2, verror.ErrUnknown},
		{0, 2, 1, 3, 2, verror.ErrUnknown},
		{1, 3, 1, 3, 3, verror.ErrUnknown},
		{3, 3, 3, 3, 3, verror.ErrUnknown},
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
		ver, err := r.CommonVersion(local, remote)
		if ver != tc.expectedVer || (err != nil && verror.ErrorID(err) != tc.expectedErr.ID) {
			t.Errorf("Unexpected result for local: %v, remote: %v.  Got (%d, %v) wanted (%d, %v)",
				local, remote, ver, err, tc.expectedVer, tc.expectedErr)
		}
		if err != nil {
			t.Logf("%s", err)
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
		expectedError          verror.IDAction
	}
	tests := []testCase{
		{0, 0, 0, 0, ErrUnknownVersion},
		{5, 10, 1, 4, ErrNoCompatibleVersion},
		{1, 4, 5, 10, ErrNoCompatibleVersion},
		{1, 10, 2, 9, verror.ErrUnknown},
		{3, 8, 1, 4, verror.ErrUnknown},
		{3, 8, 7, 9, verror.ErrUnknown},
	}

	for _, tc := range tests {
		r := &Range{Min: tc.supportMin, Max: tc.supportMax}
		remote := &inaming.Endpoint{
			MinRPCVersion: tc.remoteMin,
			MaxRPCVersion: tc.remoteMax,
		}
		err := r.CheckCompatibility(remote)
		if err != nil && verror.ErrorID(err) != tc.expectedError.ID {
			t.Errorf("Unexpected error for case %+v: got %v, wanted %v",
				tc, err, tc.expectedError)
		}
		if err != nil {
			t.Logf("%s", err)
		}
	}
}
