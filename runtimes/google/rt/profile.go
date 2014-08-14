package rt

import (
	"veyron2"
	"veyron2/config"
)

type generic struct{}

func (g *generic) Name() string {
	return "generic"
}

func (g *generic) Runtime() string {
	return veyron2.GoogleRuntimeName
}

func (g *generic) Platform() *veyron2.Platform {
	p, _ := Platform()
	return p
}

func (g *generic) Init(rt veyron2.Runtime, _ *config.Publisher) {
	rt.Logger().VI(1).Infof("%s\n", g.String())
}

func (g *generic) String() string {
	return "default generic on " + g.Platform().String()
}
