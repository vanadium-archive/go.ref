package rt

import (
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	"veyron.io/veyron/veyron2/naming"
)

func (rt *vrt) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (rt *vrt) Namespace() naming.Namespace {
	return rt.ns
}
