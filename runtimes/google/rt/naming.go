package rt

import (
	inaming "veyron/runtimes/google/naming"
	"veyron2/naming"
)

func (rt *vrt) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (rt *vrt) MountTable() naming.MountTable {
	return rt.mt
}
