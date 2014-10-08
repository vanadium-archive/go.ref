// Package sectest provides test utility functions for security-related operations for tests within veyron.io/veyron/veyron/runtimes/google/ipc/stream.
package sectest

import (
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron2/security"
)

// NewPrincipal creates a new security.Principal.
//
// It also creates self-certified blessings for defaultBlessings and
// sets them up as BlessingStore().Default() (if any are provided).
func NewPrincipal(defaultBlessings ...string) security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	var def security.Blessings
	for _, blessing := range defaultBlessings {
		b, err := p.BlessSelf(blessing)
		if err != nil {
			panic(err)
		}
		if def, err = security.UnionOfBlessings(def, b); err != nil {
			panic(err)
		}
	}
	if def != nil {
		if err := p.BlessingStore().SetDefault(def); err != nil {
			panic(err)
		}
		if _, err := p.BlessingStore().Set(def, security.AllPrincipals); err != nil {
			panic(err)
		}
		if err := p.AddToRoots(def); err != nil {
			panic(err)
		}
	}
	return p
}
