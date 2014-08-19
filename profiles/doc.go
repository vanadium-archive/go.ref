// Package Profiles, and its children, provide implementations of the
// veyron2.Profile interface. These implementations should import all of the
// packages that they require to implement Profile-specific functionality.
//
// The taxonomy used to organise Profiles may be arbitrary and the directory
// structure used here is just one convention for how to do so. This directory
// structure reflects the generality of a Profile at any given depth in the
// hierarchy, with higher levels of the directory structure being more
// generic and lower levels more specific.
//
// Profiles register themselves by calling veyron2/rt.RegisterProfile in their
// init function and are hence are chosen by importing them into an
// applications main package. More specific packages may use functionality
// exposed by more general packages and rely on go's module dependency
// algorithm to execute the init function from the more specific package
// after the less specific one and hence override the earlier Profile
// registration.
//
// This top level directory contains a 'generic' Profile and utility routines
// used by other Profiles. It does not follow the convention of registering
// itself via its Init function, since the expected use is that it will
// used automatically as a default by the Runtime. Instead it provides a New
// function. This avoids the need for every main package to import
// "veyron/profiles", instead, only more specific Profiles must be so imported.
//
// The 'net' Profile adds operating system support for varied network
// configurations and in particular dhcp. It should be used by any application
// that may 'roam' or any may be behind a 1-1 NAT.
//
// The 'net/bluetooth' Profile adds operating system support for bluetooth
// networking.
// TODO(cnicolaou,ashankar): add this
package profiles

import (
	"veyron2"
	"veyron2/config"
)

type generic struct{}

// New returns a new instance of a very generic Profile. It can be used
// as a default by Runtime implementations, in unit tests etc.
func New() veyron2.Profile {
	return &generic{}
}

func (g *generic) Name() string {
	return "generic"
}

func (g *generic) Runtime() string {
	return ""
}

func (g *generic) Platform() *veyron2.Platform {
	p, _ := Platform()
	return p
}

func (g *generic) Init(rt veyron2.Runtime, _ *config.Publisher) {
	rt.Logger().VI(1).Infof("%s", g)
}

func (g *generic) String() string {
	return "generic profile on " + g.Platform().String()
}
