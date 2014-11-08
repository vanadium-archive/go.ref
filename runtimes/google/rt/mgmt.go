package rt

import (
	"fmt"
	"os"
	"sync"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/mgmt"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/runtimes/google/appcycle"
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

func getListenSpec(handle *exec.ChildHandle) (*ipc.ListenSpec, error) {
	protocol, err := handle.Config.Get(mgmt.ProtocolConfigKey)
	if err != nil {
		return nil, err
	}
	if protocol == "" {
		return nil, fmt.Errorf("%v is not set", mgmt.ProtocolConfigKey)
	}

	address, err := handle.Config.Get(mgmt.AddressConfigKey)
	if err != nil {
		return nil, err
	}
	if address == "" {
		return nil, fmt.Errorf("%v is not set", mgmt.AddressConfigKey)
	}
	return &ipc.ListenSpec{Protocol: protocol, Address: address}, nil
}

func (m *mgmtImpl) initMgmt(rt *vrt) error {
	// Do not initialize the mgmt runtime if the process has not
	// been started through the veyron exec library by a node
	// manager.
	handle, err := exec.GetChildHandle()
	if err != nil {
		return nil
	}
	parentName, err := handle.Config.Get(mgmt.ParentNameConfigKey)
	if err != nil {
		return nil
	}
	listenSpec, err := getListenSpec(handle)
	if err != nil {
		return err
	}
	var serverOpts []ipc.ServerOpt
	parentPeerPattern, err := handle.Config.Get(mgmt.ParentBlessingConfigKey)
	if err == nil && parentPeerPattern != "" {
		// Grab the blessing from our blessing store that the parent
		// told us to use so they can talk to us.
		serverBlessing := rt.Principal().BlessingStore().ForPeer(parentPeerPattern)
		serverOpts = append(serverOpts, options.ServerBlessings{serverBlessing})
	}
	m.rt = rt
	m.server, err = rt.NewServer(serverOpts...)
	if err != nil {
		return err
	}
	ep, err := m.server.Listen(*listenSpec)
	if err != nil {
		return err
	}
	if err := m.server.Serve("", appcycle.AppCycleServer(m), nil); err != nil {
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

func (m *mgmtImpl) Stop(ctx appcycle.AppCycleStopContext) error {
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
		ctx.SendStream().Send(task)
	}
	return nil
}

func (m *mgmtImpl) ForceStop(ipc.ServerContext) error {
	m.rt.ForceStop()
	return fmt.Errorf("ForceStop should not reply as the process should be dead")
}
