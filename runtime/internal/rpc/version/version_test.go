// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package version

import (
	"testing"

	"v.io/v23/rpc/version"
	"v.io/v23/verror"
)

func TestIntersect(t *testing.T) {
	type testCase struct {
		localMin, localMax   version.RPCVersion
		remoteMin, remoteMax version.RPCVersion
		expected             *Range
		expectedErr          verror.IDAction
	}
	tests := []testCase{
		{0, 0, 0, 0, nil, ErrUnknownVersion},
		{0, 2, 3, 4, nil, ErrNoCompatibleVersion},
		{3, 4, 0, 2, nil, ErrNoCompatibleVersion},
		{0, 6, 6, 7, nil, ErrNoCompatibleVersion},
		{0, 3, 3, 5, &Range{3, 3}, verror.ErrUnknown},
		{0, 3, 2, 4, &Range{2, 3}, verror.ErrUnknown},
		{2, 4, 2, 4, &Range{2, 4}, verror.ErrUnknown},
		{4, 4, 4, 4, &Range{4, 4}, verror.ErrUnknown},
	}
	for _, tc := range tests {
		local := &Range{
			Min: tc.localMin,
			Max: tc.localMax,
		}
		remote := &Range{
			Min: tc.remoteMin,
			Max: tc.remoteMax,
		}
		intersection, err := local.Intersect(remote)

		if (tc.expected != nil && *tc.expected != *intersection) ||
			(err != nil && verror.ErrorID(err) != tc.expectedErr.ID) {
			t.Errorf("Unexpected result for local: %v, remote: %v.  Got (%v, %v) wanted (%v, %v)",
				local, remote, intersection, err,
				tc.expected, tc.expectedErr)
		}
		if err != nil {
			t.Logf("%s", err)
		}
	}
}
