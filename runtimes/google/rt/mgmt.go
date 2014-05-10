package rt

import (
	"veyron/runtimes/google/mgmt"
)

func (rt *vrt) initMgmt() {
	rt.mgmtRT = mgmt.New()
}
