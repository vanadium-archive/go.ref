package impl

import (
	"errors"
	"os/exec"
	"runtime"
	"time"

	"veyron2/ipc"
	"veyron2/vlog"
)

// invoker is the object that implements the Root interface
type invoker struct{}

// NewInvoker is the invoker factory.
func NewInvoker() *invoker {
	return &invoker{}
}

// ROOT INTERFACE IMPLEMENTATION

// resetLinux implements the Reset method for Linux.
func (i *invoker) resetLinux(deadline uint64) {
	time.Sleep(time.Duration(deadline) * time.Millisecond)
	cmd := exec.Command("shutdown", "-r", "now")
	vlog.VI(0).Infof("Shutting down.")
	cmd.Run()
}

func (i *invoker) Reset(call ipc.ServerContext, deadline uint64) error {
	vlog.VI(0).Infof("Reset(%v).", deadline)
	switch runtime.GOOS {
	case "linux":
		go i.resetLinux(deadline)
	default:
		// TODO(jsimsa): Implement Reset method for additional operating
		// systems.
		return errors.New("Unsupported operating system: " + runtime.GOOS)
	}
	return nil
}
