package ipc

import (
	"sync"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
)

const nilRuntimeMessage = "attempting to create a context with a nil runtime"

// InternalNewContext creates a new context.T.  This function should only
// be called from within the runtime implementation.
func InternalNewContext(runtime veyron2.Runtime) context.T {
	if runtime == nil {
		panic(nilRuntimeMessage)
	}
	return rootContext{runtime}
}

// cancellable is an interface to cancellable contexts.
type cancellable interface {
	// cancel cancels the context and records the given error.
	cancel(err error)
	addChild(child cancellable)
	removeChild(parent cancellable)
}

// child is an interface that allows you to find the nearest cancelContext ancestor.
type child interface {
	parent() context.T
}

// rootContext is an empty root context.  It has no deadline or values
// and can't be canceled.
type rootContext struct {
	runtime veyron2.Runtime
}

func (r rootContext) parent() context.T                       { return nil }
func (r rootContext) Deadline() (deadline time.Time, ok bool) { return }
func (r rootContext) Done() <-chan struct{}                   { return nil }
func (r rootContext) Err() error                              { return nil }
func (r rootContext) Value(key interface{}) interface{}       { return nil }
func (r rootContext) Runtime() interface{}                    { return r.runtime }
func (r rootContext) WithCancel() (ctx context.T, cancel context.CancelFunc) {
	return newCancelContext(r)
}
func (r rootContext) WithDeadline(deadline time.Time) (context.T, context.CancelFunc) {
	return newDeadlineContext(r, deadline)
}
func (r rootContext) WithTimeout(timeout time.Duration) (context.T, context.CancelFunc) {
	return newDeadlineContext(r, time.Now().Add(timeout))
}
func (r rootContext) WithValue(key interface{}, val interface{}) context.T {
	return newValueContext(r, key, val)
}

// A valueContext contains a single key/value mapping.
type valueContext struct {
	context.T
	key, value interface{}
}

func newValueContext(parent context.T, key, val interface{}) *valueContext {
	return &valueContext{parent, key, val}
}

func (v *valueContext) parent() context.T {
	return v.T
}
func (v *valueContext) Value(key interface{}) interface{} {
	if key == v.key {
		return v.value
	}
	return v.T.Value(key)
}
func (v *valueContext) WithCancel() (ctx context.T, cancel context.CancelFunc) {
	return newCancelContext(v)
}
func (v *valueContext) WithDeadline(deadline time.Time) (context.T, context.CancelFunc) {
	return newDeadlineContext(v, deadline)
}
func (v *valueContext) WithTimeout(timeout time.Duration) (context.T, context.CancelFunc) {
	return newDeadlineContext(v, time.Now().Add(timeout))
}
func (v *valueContext) WithValue(key interface{}, val interface{}) context.T {
	return newValueContext(v, key, val)
}

// A cancelContext provides a mechanism for cancellation and a
// done channel that allows it's status to be monitored.
type cancelContext struct {
	context.T
	done chan struct{}
	err  error

	// children is used to keep track of descendant cancellable
	// contexts. This is an optimization to prevent excessive
	// goroutines.
	children map[cancellable]bool

	mu sync.Mutex
}

func newCancelContext(parent context.T) (ctx *cancelContext, cancel context.CancelFunc) {
	ctx = &cancelContext{
		T:    parent,
		done: make(chan struct{}),
	}

	cancel = func() { ctx.cancel(context.Canceled) }
	if parent.Done() == nil {
		return
	}

	if ancestor, nonStandardAncestor := ctx.findCancellableAncestor(); !nonStandardAncestor {
		if ancestor != nil {
			ancestor.addChild(ctx)
		}
		return
	}

	// If neither the parent nor the child are canceled then both the
	// parent and the child will leak. Note this will only happen for
	// non-standard implementations of the context.T interface.
	go func() {
		select {
		case <-parent.Done():
			ctx.cancel(parent.Err())
		case <-ctx.Done():
		}
	}()

	return
}

// addChild sets child as a descendant cancellable context. This
// allows us to propagate cancellations through the context tree.
func (c *cancelContext) addChild(child cancellable) {
	c.mu.Lock()
	if c.err != nil {
		// If the parent is already canceled, just cancel the child.
		c.mu.Unlock()
		child.cancel(c.err)
		return
	}
	defer c.mu.Unlock()
	if c.children == nil {
		c.children = make(map[cancellable]bool)
	}
	c.children[child] = true
}

// removeChild is called by descendant contexts when they are
// canceled.  This prevents old contexts which are no longer relevant
// from consuming resources.
func (c *cancelContext) removeChild(child cancellable) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.children, child)
}

// cancelChildren cancels all descendant cancellable contexts. This
// is called during cancel but while mu is NOT held. Children may try
// to make calls to parents, which would result in a deadlock.
func cancelChildren(children map[cancellable]bool, err error) {
	for child, _ := range children {
		child.cancel(err)
	}
}

// cancel cancels the context, propagates that signal to children,
// and updates parents.
func (c *cancelContext) cancel(err error) {
	if err == nil {
		panic("Context canceled with nil error.")
	}
	c.mu.Lock()
	// cancelChilren should be called after mu is released.
	defer cancelChildren(c.children, err)
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	c.children = nil
	if ancestor, nonStandardAncestor := c.findCancellableAncestor(); !nonStandardAncestor {
		if ancestor != nil {
			ancestor.removeChild(c)
		}
	}
	close(c.done)
}

// findCancelAncestor finds the nearest ancestor that supports cancellation.
// nonStandardAncestor will be true if we cannot determine if there is a cancellable
// ancestor due to the presence of an unknown context implementation.  In this
// case ancestor will always be nil.
func (c *cancelContext) findCancellableAncestor() (ancestor cancellable, nonStandardAncestor bool) {
	parent := c.T
	for {
		if c, ok := parent.(cancellable); ok {
			return c, false
		}
		c, ok := parent.(child)
		if !ok {
			return nil, true
		}
		parent = c.parent()
	}
	return nil, false // Unreachable.
}

func (c *cancelContext) Done() <-chan struct{} { return c.done }
func (c *cancelContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
func (c *cancelContext) WithCancel() (ctx context.T, cancel context.CancelFunc) {
	return newCancelContext(c)
}
func (c *cancelContext) WithDeadline(deadline time.Time) (context.T, context.CancelFunc) {
	return newDeadlineContext(c, deadline)
}
func (c *cancelContext) WithTimeout(timeout time.Duration) (context.T, context.CancelFunc) {
	return newDeadlineContext(c, time.Now().Add(timeout))
}
func (c *cancelContext) WithValue(key interface{}, val interface{}) context.T {
	return newValueContext(c, key, val)
}

// A deadlineContext automatically cancels itself when the deadline is reached.
type deadlineContext struct {
	*cancelContext
	deadline time.Time
	timer    *time.Timer
}

// newDeadlineContext returns a new deadlineContext.
func newDeadlineContext(parent context.T, deadline time.Time) (*deadlineContext, context.CancelFunc) {
	cancel, _ := newCancelContext(parent)
	ctx := &deadlineContext{
		cancelContext: cancel,
		deadline:      deadline,
	}
	delta := deadline.Sub(time.Now())
	ctx.timer = time.AfterFunc(delta, func() { cancel.cancel(context.DeadlineExceeded) })
	return ctx, func() { ctx.cancel(context.Canceled) }
}

// cancel cancels the deadlineContext, forwards the signal to
// descendants, and notifies parents.
func (d *deadlineContext) cancel(err error) {
	d.timer.Stop()
	d.cancelContext.cancel(err)
}
func (d *deadlineContext) Deadline() (deadline time.Time, ok bool) { return d.deadline, true }
func (d *deadlineContext) WithCancel() (ctx context.T, cancel context.CancelFunc) {
	return newCancelContext(d)
}
func (d *deadlineContext) WithDeadline(deadline time.Time) (context.T, context.CancelFunc) {
	return newDeadlineContext(d, deadline)
}
func (d *deadlineContext) WithTimeout(timeout time.Duration) (context.T, context.CancelFunc) {
	return newDeadlineContext(d, time.Now().Add(timeout))
}
func (d *deadlineContext) WithValue(key interface{}, val interface{}) context.T {
	return newValueContext(d, key, val)
}
