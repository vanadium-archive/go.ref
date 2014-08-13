package version

import (
	"fmt"

	inaming "veyron/runtimes/google/naming"

	"veyron2/ipc/version"
	"veyron2/naming"
)

// Range represents a range of IPC versions.
type Range struct {
	Min, Max version.IPCVersion
}

var (
	// supportedRange represents the range of protocol verions supported by this
	// implementation.
	// Max should be incremented whenever we make a protocol
	// change that's not both forward and backward compatible.
	// Min should be incremented whenever we want to remove
	// support for old protocol versions.
	supportedRange = &Range{Min: version.IPCVersion1, Max: version.IPCVersion3}

	// Export the methods on supportedRange.
	Endpoint           = supportedRange.Endpoint
	ProxiedEndpoint    = supportedRange.ProxiedEndpoint
	CommonVersion      = supportedRange.CommonVersion
	CheckCompatibility = supportedRange.CheckCompatibility
)

var (
	NoCompatibleVersionErr = fmt.Errorf("No compatible IPC version available")
	UnknownVersionErr      = fmt.Errorf("There was not enough information to determine a version.")
)

// IsVersionError returns true if err is a versioning related error.
func IsVersionError(err error) bool {
	return err == NoCompatibleVersionErr || err == UnknownVersionErr
}

// Endpoint returns an endpoint with the Min/MaxIPCVersion properly filled in
// to match this implementations supported protocol versions.
func (r *Range) Endpoint(protocol, address string, rid naming.RoutingID) naming.Endpoint {
	return &inaming.Endpoint{
		Protocol:      protocol,
		Address:       address,
		RID:           rid,
		MinIPCVersion: r.Min,
		MaxIPCVersion: r.Max,
	}
}

// intersectRanges finds the intersection between ranges
// supported by two endpoints.  We make an assumption here that if one
// of the endpoints has an UnknownVersion we assume it has the same
// extent as the other endpoint. If both endpoints have Unknown for a
// version number, an error is produced.
// For example:
//   a == (2, 4) and b == (Unknown, Unknown), intersect(a,b) == (2, 4)
//   a == (2, Unknown) and b == (3, 4), intersect(a,b) == (3, 4)
func intersectRanges(amin, amax, bmin, bmax version.IPCVersion) (min, max version.IPCVersion, err error) {
	u := version.UnknownIPCVersion

	min = amin
	if min == u || (bmin != u && bmin > min) {
		min = bmin
	}
	max = amax
	if max == u || (bmax != u && bmax < max) {
		max = bmax
	}

	if min == u || max == u {
		err = UnknownVersionErr
	} else if min > max {
		err = NoCompatibleVersionErr
	}
	return
}

func intersectEndpoints(a, b *inaming.Endpoint) (min, max version.IPCVersion, err error) {
	return intersectRanges(a.MinIPCVersion, a.MaxIPCVersion, b.MinIPCVersion, b.MaxIPCVersion)
}

// ProxiedEndpoint returns an endpoint with the Min/MaxIPCVersion properly filled in
// to match the intersection of capabilities of this process and the proxy.
func (r *Range) ProxiedEndpoint(rid naming.RoutingID, proxy naming.Endpoint) (naming.Endpoint, error) {
	proxyEP, ok := proxy.(*inaming.Endpoint)
	if !ok {
		return nil, fmt.Errorf("unrecognized naming.Endpoint type %T", proxy)
	}

	ep := &inaming.Endpoint{
		Protocol:      proxyEP.Protocol,
		Address:       proxyEP.Address,
		RID:           rid,
		MinIPCVersion: r.Min,
		MaxIPCVersion: r.Max,
	}

	// This is the endpoint we are going to advertise.  It should only claim to support versions in
	// the intersection of those we support and those the proxy supports.
	var err error
	ep.MinIPCVersion, ep.MaxIPCVersion, err = intersectEndpoints(ep, proxyEP)
	if err != nil {
		return nil, fmt.Errorf("attempting to register with incompatible proxy: %s", proxy)
	}
	return ep, nil
}

// CommonVersion determines which version of the IPC protocol should be used
// between two endpoints.  Returns an error if the resulting version is incompatible
// with this IPC implementation.
func (r *Range) CommonVersion(a, b naming.Endpoint) (version.IPCVersion, error) {
	aEP, ok := a.(*inaming.Endpoint)
	if !ok {
		return 0, fmt.Errorf("Unrecognized naming.Endpoint type: %T", a)
	}
	bEP, ok := b.(*inaming.Endpoint)
	if !ok {
		return 0, fmt.Errorf("Unrecognized naming.Endpoint type: %T", b)
	}

	_, max, err := intersectEndpoints(aEP, bEP)
	if err != nil {
		return 0, err
	}

	// We want to use the maximum common version of the protocol.  We just
	// need to make sure that it is supported by this IPC implementation.
	if max < r.Min || max > r.Max {
		return version.UnknownIPCVersion, NoCompatibleVersionErr
	}
	return max, nil
}

// CheckCompatibility returns an error if the given endpoint is incompatible
// with this IPC implementation.  It returns nil otherwise.
func (r *Range) CheckCompatibility(remote naming.Endpoint) error {
	remoteEP, ok := remote.(*inaming.Endpoint)
	if !ok {
		return fmt.Errorf("Unrecognized naming.Endpoint type: %T", remote)
	}

	_, _, err := intersectRanges(r.Min, r.Max,
		remoteEP.MinIPCVersion, remoteEP.MaxIPCVersion)

	return err
}
