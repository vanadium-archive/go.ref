package fake

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"

	"v.io/core/veyron/runtimes/fake"
)

func init() {
	veyron2.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (veyron2.Runtime, *context.T, veyron2.Shutdown, error) {
	return fake.Init(ctx)
}
