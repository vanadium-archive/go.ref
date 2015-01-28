package impl_test

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	"v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron/tools/mgmt/device/impl"
)

var gctx *context.T

func initTest() veyron2.Shutdown {
	ctx, shutdown := veyron2.Init()
	var err error
	if ctx, err = veyron2.SetPrincipal(ctx, security.NewPrincipal("test-blessing")); err != nil {
		panic(err)
	}
	gctx = ctx
	impl.SetGlobalContext(gctx)
	return func() {
		shutdown()
		impl.SetGlobalContext(nil)
		gctx = nil
	}
}
