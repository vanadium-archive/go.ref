package fake

import (
	"v.io/v23"
	"v.io/v23/context"
)

func init() {
	v23.RegisterProfileInit(Init)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	return new(ctx)
}
