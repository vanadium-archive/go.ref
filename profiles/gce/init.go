// +build linux

// Package gce provides a Profile for Google Compute Engine and should be
// used by binaries that only ever expect to be run on GCE.
package gce

import (
	"veyron/profiles"

	"veyron2"
	"veyron2/config"
	"veyron2/rt"

	"veyron/profiles/internal/gce"
)

func init() {
	rt.RegisterProfile(&profile{})
}

type profile struct{}

func (p *profile) Name() string {
	return "GCE"
}

func (p *profile) Runtime() string {
	return ""
}

func (p *profile) Platform() *veyron2.Platform {
	platform, _ := profiles.Platform()
	return platform
}

func (p *profile) String() string {
	return "net " + p.Platform().String()
}

func (p *profile) Init(rt veyron2.Runtime, publisher *config.Publisher) {
	if !gce.RunningOnGCE() {
		panic("GCE profile used on a non-GCE system")
	}
	go addressPublisher(rt, publisher)
}

func addressPublisher(rt veyron2.Runtime, p *config.Publisher) {
	ch := make(chan config.Setting)
	p.CreateStream("net", "network configuration", ch)
	for {
		// TODO(cnicolaou): publish local and external address..
	}
}
