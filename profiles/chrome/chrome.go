// Package chrome implements a profile for use within Chrome, in particular
// for use by Chrome extensions.
package chrome

import (
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/profiles/internal/platform"
	_ "veyron.io/veyron/veyron/runtimes/google/ipc/protocols/ws"
	_ "veyron.io/veyron/veyron/runtimes/google/rt"
)

var ListenSpec = ipc.ListenSpec{}

type chrome struct{}

func init() {
	rt.RegisterProfile(New())
}

// New returns a new instance of a Profile for use within chrome, in particular
// chrome extensions etc should use.
func New() veyron2.Profile {
	return &chrome{}
}

func (*chrome) Name() string {
	return "chrome"
}

func (*chrome) Runtime() (string, []veyron2.ROpt) {
	return veyron2.GoogleRuntimeName, []veyron2.ROpt{options.PreferredProtocols{"ws"}}
}

func (*chrome) Platform() *veyron2.Platform {
	p, _ := platform.Platform()
	return p
}

func (c *chrome) Init(rt veyron2.Runtime, _ *config.Publisher) (veyron2.AppCycle, error) {
	rt.Logger().VI(1).Infof("%s", c)
	return nil, nil
}

func (*chrome) Cleanup() {}

func (c *chrome) String() string {
	return "chrome profile on " + c.Platform().String()
}
