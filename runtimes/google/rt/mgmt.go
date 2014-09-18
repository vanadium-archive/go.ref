package rt

import (
	"fmt"
	"os"
	"sync"
	"time"

	"veyron.io/veyron/veyron/runtimes/google/appcycle"
	"veyron.io/veyron/veyron/services/mgmt/lib/exec"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/mgmt"
	"veyron.io/veyron/veyron2/naming"
)

type mgmtImpl struct {
	sync.RWMutex
	waiters      []chan<- string
	taskTrackers []chan<- veyron2.Task
	task         veyron2.Task
	shutDown     bool
	rt           *vrt
	server       ipc.Server // Serves AppCycle service.
}

// parentName returns the object name for the Config service on which we should
// communicate the object name of the app cycle service.  Currently, this can
// be configured either via env vars or via the exec config passed from parent.
func parentName() (name string) {
	name = os.Getenv(mgmt.ParentNodeManagerConfigKey)
	if len(name) > 0 {
		return
	}
	handle, _ := exec.GetChildHandle()
	if handle == nil {
		return
	}
	name, _ = handle.Config.Get(mgmt.ParentNodeManagerConfigKey)
	return
}

func (m *mgmtImpl) init(rt *vrt) error {
	m.rt = rt
	parentName := parentName()
	if len(parentName) == 0 {
		return nil
	}
	var err error
	if m.server, err = rt.NewServer(); err != nil {
		return err
	}
	// TODO(caprita): We should pick the address to listen on from config.
	var ep naming.Endpoint
	if ep, err = m.server.Listen("tcp", "127.0.0.1:0"); err != nil {
		return err
	}
	if err := m.server.Serve("", ipc.LeafDispatcher(appcycle.NewServerAppCycle(m), nil)); err != nil {
		return err
	}
	return m.callbackToParent(parentName, naming.JoinAddressName(ep.String(), ""))
}

func (m *mgmtImpl) callbackToParent(parentName, myName string) error {
	ctx, _ := m.rt.NewContext().WithTimeout(10 * time.Second)
	call, err := m.rt.Client().StartCall(ctx, parentName, "Set", []interface{}{mgmt.AppCycleManagerConfigKey, myName})
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}

func (m *mgmtImpl) shutdown() {
	m.Lock()
	if m.shutDown {
		m.Unlock()
		return
	}
	m.shutDown = true
	for _, t := range m.taskTrackers {
		close(t)
	}
	m.taskTrackers = nil
	server := m.server
	m.Unlock()
	if server != nil {
		server.Stop()
	}
}

func (rt *vrt) stop(msg string) {
	rt.mgmt.RLock()
	defer rt.mgmt.RUnlock()
	if len(rt.mgmt.waiters) == 0 {
		os.Exit(veyron2.UnhandledStopExitCode)
	}
	for _, w := range rt.mgmt.waiters {
		select {
		case w <- msg:
		default:
		}
	}
}

func (rt *vrt) Stop() {
	rt.stop(veyron2.LocalStop)
}

func (*vrt) ForceStop() {
	os.Exit(veyron2.ForceStopExitCode)
}

func (rt *vrt) WaitForStop(ch chan<- string) {
	rt.mgmt.Lock()
	defer rt.mgmt.Unlock()
	rt.mgmt.waiters = append(rt.mgmt.waiters, ch)
}

func (rt *vrt) TrackTask(ch chan<- veyron2.Task) {
	rt.mgmt.Lock()
	defer rt.mgmt.Unlock()
	if rt.mgmt.shutDown {
		close(ch)
		return
	}
	rt.mgmt.taskTrackers = append(rt.mgmt.taskTrackers, ch)
}

func (rt *vrt) advanceTask(progress, goal int) {
	rt.mgmt.Lock()
	defer rt.mgmt.Unlock()
	rt.mgmt.task.Goal += goal
	rt.mgmt.task.Progress += progress
	for _, t := range rt.mgmt.taskTrackers {
		select {
		case t <- rt.mgmt.task:
		default:
			// TODO(caprita): Make it such that the latest task
			// update is always added to the channel even if channel
			// is full.  One way is to pull an element from t and
			// then re-try the push.
		}
	}
}

func (rt *vrt) AdvanceGoal(delta int) {
	if delta <= 0 {
		return
	}
	rt.advanceTask(0, delta)
}

func (rt *vrt) AdvanceProgress(delta int) {
	if delta <= 0 {
		return
	}
	rt.advanceTask(delta, 0)
}

func (m *mgmtImpl) Stop(_ ipc.ServerContext, stream appcycle.AppCycleServiceStopStream) error {
	// The size of the channel should be reasonably sized to expect not to
	// miss updates while we're waiting for the stream to unblock.
	ch := make(chan veyron2.Task, 10)
	m.rt.TrackTask(ch)
	// TODO(caprita): Include identity of Stop issuer in message.
	m.rt.stop(veyron2.RemoteStop)
	for {
		task, ok := <-ch
		if !ok {
			// Channel closed, meaning process shutdown is imminent.
			break
		}
		stream.Send(task)
	}
	return nil
}

func (m *mgmtImpl) ForceStop(ipc.ServerContext) error {
	m.rt.ForceStop()
	return fmt.Errorf("ForceStop should not reply as the process should be dead")
}
