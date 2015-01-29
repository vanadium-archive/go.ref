package impl_test

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	"v.io/core/veyron/lib/testutil"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron/tools/mgmt/device/impl"
)

var gctx *context.T

func initTest() veyron2.Shutdown {
	var shutdown veyron2.Shutdown
	gctx, shutdown = testutil.InitForTest()
	impl.SetGlobalContext(gctx)
	return func() {
		shutdown()
		impl.SetGlobalContext(nil)
		gctx = nil
	}
}
