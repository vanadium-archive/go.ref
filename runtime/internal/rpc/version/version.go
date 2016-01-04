// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package version

import (
	"fmt"

	"v.io/v23/rpc/version"
	"v.io/v23/verror"
	"v.io/x/lib/metadata"
)

// Range represents a range of RPC versions.
type Range struct {
	Min, Max version.RPCVersion
}

// Supported represents the range of protocol verions supported by this
// implementation.
//
// Max is incremented whenever we make a protocol change that's not both forward
// and backward compatible.
//
// Min is incremented whenever we want to remove support for old protocol
// versions.
var Supported = version.RPCVersionRange{Min: version.RPCVersion10, Max: version.RPCVersion14}

func init() {
	metadata.Insert("v23.RPCVersionMax", fmt.Sprint(Supported.Max))
	metadata.Insert("v23.RPCVersionMin", fmt.Sprint(Supported.Min))
}

const pkgPath = "v.io/x/ref/runtime/internal/rpc/version"

func reg(id, msg string) verror.IDAction {
	return verror.Register(verror.ID(pkgPath+id), verror.NoRetry, msg)
}

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	ErrNoCompatibleVersion = reg(".errNoCompatibleVersionErr", "no compatible RPC version available{:3} not in range {4}..{5}")
	ErrUnknownVersion      = reg(".errUnknownVersionErr", "there was not enough information to determine a version")
	ErrDeprecatedVersion   = reg(".errDeprecatedVersionError", "some of the provided version information is deprecated")
)

// IsVersionError returns true if err is a versioning related error.
func IsVersionError(err error) bool {
	id := verror.ErrorID(err)
	return id == ErrNoCompatibleVersion.ID || id == ErrUnknownVersion.ID || id == ErrDeprecatedVersion.ID
}

// intersectRanges finds the intersection between ranges
// supported by two endpoints.  We make an assumption here that if one
// of the endpoints has an UnknownVersion we assume it has the same
// extent as the other endpoint. If both endpoints have Unknown for a
// version number, an error is produced.
// For example:
//   a == (2, 4) and b == (Unknown, Unknown), intersect(a,b) == (2, 4)
//   a == (2, Unknown) and b == (3, 4), intersect(a,b) == (3, 4)
func intersectRanges(amin, amax, bmin, bmax version.RPCVersion) (min, max version.RPCVersion, err error) {
	// TODO(mattr): this may be incorrect.  Ensure that when we talk to a server who
	// advertises (5,8) and we support (5, 9) but v5 EPs (so we may get d, d here) that
	// we use v8 and don't send setupVC.
	d := version.DeprecatedRPCVersion
	if amin == d || amax == d || bmin == d || bmax == d {
		return d, d, verror.New(ErrDeprecatedVersion, nil)
	}

	u := version.UnknownRPCVersion

	min = amin
	if min == u || (bmin != u && bmin > min) {
		min = bmin
	}
	max = amax
	if max == u || (bmax != u && bmax < max) {
		max = bmax
	}

	if min == u || max == u {
		err = verror.New(ErrUnknownVersion, nil)
	} else if min > max {
		err = verror.New(ErrNoCompatibleVersion, nil, u, min, max)
	}
	return
}

func (r1 *Range) Intersect(r2 *Range) (*Range, error) {
	min, max, err := intersectRanges(r1.Min, r1.Max, r2.Min, r2.Max)
	if err != nil {
		return nil, err
	}
	r := &Range{Min: min, Max: max}
	return r, nil
}
