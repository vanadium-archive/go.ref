// fake implements a fake runtime.  The fake runtime is useful in tests when you
// want to mock out important components.
// TODO(mattr): Make a more complete, but still fake, implementation.
package fake

import (
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"

	tsecurity "v.io/core/veyron/lib/testutil/security"
)

type contextKey int

const (
	clientKey = contextKey(iota)
	principalKey
	loggerKey
	backgroundKey
)

type Runtime struct{}

func Init(ctx *context.T) (*Runtime, *context.T, veyron2.Shutdown, error) {
	ctx = context.WithValue(ctx, principalKey, tsecurity.NewPrincipal())
	return &Runtime{}, ctx, func() {}, nil
}

func (r *Runtime) Init(ctx *context.T) error {
	return nil
}

func (r *Runtime) SetPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	return context.WithValue(ctx, principalKey, principal), nil
}

func (r *Runtime) GetPrincipal(ctx *context.T) security.Principal {
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (r *Runtime) SetNewLogger(ctx *context.T, name string, opts ...vlog.LoggingOpts) (*context.T, vlog.Logger, error) {
	logger, err := vlog.NewLogger(name, opts...)
	if err != nil {
		return context.WithValue(ctx, loggerKey, logger), logger, nil
	}
	return ctx, nil, err
}

func (r *Runtime) GetLogger(ctx *context.T) vlog.Logger {
	l, _ := ctx.Value(loggerKey).(vlog.Logger)
	return l
}

func (r *Runtime) GetAppCycle(ctx *context.T) veyron2.AppCycle {
	panic("unimplemented")
}

func (r *Runtime) SetBackgroundContext(ctx *context.T) *context.T {
	// Note we add an extra context with a nil value here.
	// This prevents users from travelling back through the
	// chain of background contexts.
	ctx = context.WithValue(ctx, backgroundKey, nil)
	return context.WithValue(ctx, backgroundKey, ctx)
}

func (r *Runtime) GetBackgroundContext(ctx *context.T) *context.T {
	bctx, _ := ctx.Value(backgroundKey).(*context.T)
	if bctx == nil {
		// There should always be a background context.  If we don't find
		// it, that means that the user passed us the background context
		// in hopes of following the chain.  Instead we just give them
		// back what they sent in, which is correct.
		return ctx
	}
	return bctx
}
