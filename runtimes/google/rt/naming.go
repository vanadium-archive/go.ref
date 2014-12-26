package rt

import (
	inaming "v.io/core/veyron/runtimes/google/naming"
	"v.io/core/veyron2/naming"
)

func (rt *vrt) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (rt *vrt) Namespace() naming.Namespace {
	return rt.ns
}
