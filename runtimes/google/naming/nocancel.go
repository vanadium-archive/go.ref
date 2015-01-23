package naming

// NoCancel is an option passed to a resolve or an IPC call that
// instructs it to ignore cancellation for that call.
// This is used to allow servers to unmount even after their context
// has been cancelled.
// TODO(mattr): Find a better mechanism for this.
type NoCancel struct{}

func (NoCancel) IPCCallOpt()   {}
func (NoCancel) NSResolveOpt() {}
