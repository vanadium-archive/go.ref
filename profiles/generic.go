package profiles

import (
	"veyron2"
	"veyron2/config"

	"veyron/profiles/internal"
)

type generic struct{}

// New returns a new instance of a very generic Profile. It can be used
// as a default by Runtime implementations, in unit tests etc.
func New() veyron2.Profile {
	return &generic{}
}

func (*generic) Name() string {
	return "generic"
}

func (*generic) Runtime() string {
	return ""
}

func (*generic) Platform() *veyron2.Platform {
	p, _ := Platform()
	return p
}

func (*generic) AddressChooser() veyron2.AddressChooser {
	return internal.IPAddressChooser
}

func (g *generic) Init(rt veyron2.Runtime, _ *config.Publisher) {
	rt.Logger().VI(1).Infof("%s", g)
}

func (g *generic) String() string {
	return "generic profile on " + g.Platform().String()
}
