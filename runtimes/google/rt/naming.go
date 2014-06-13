package rt

import (
	inaming "veyron/runtimes/google/naming"
	"veyron2/naming"
)

func (rt *vrt) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (rt *vrt) Namespace() naming.Namespace {
	return rt.ns
}
