package rt

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"veyron/runtimes/google/naming/namespace"
	"veyron/services/mgmt/lib/exec"

	"veyron2"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/product"
	"veyron2/security"
	"veyron2/vlog"
)

type vrt struct {
	product product.T
	sm      stream.Manager
	ns      naming.Namespace
	signals chan os.Signal
	id      security.PrivateID
	store   security.PublicIDStore
	client  ipc.Client
	mgmt    *mgmtImpl
	debug   debugServer

	mu       sync.Mutex
	nServers int // GUARDED_BY(mu)
}

var (
	globalR   veyron2.Runtime
	globalErr error
	once      sync.Once
)

// Implements veyron2/rt.New
func New(opts ...veyron2.ROpt) (veyron2.Runtime, error) {
	r := &vrt{mgmt: new(mgmtImpl)}
	return r, r.init(opts...)
}

// Implements veyron2/rt.R
func R() veyron2.Runtime {
	return globalR
}

// Used to implement veyron2/rt.Init
func Init(opts ...veyron2.ROpt) (veyron2.Runtime, error) {
	once.Do(func() {
		globalR, globalErr = New(opts...)
	})
	return globalR, globalErr
}

// init initalizes the runtime instance it is invoked on.
func (rt *vrt) init(opts ...veyron2.ROpt) error {
	flag.Parse()
	rt.initHTTPDebugServer()
	nsRoots := []string{}
	for _, o := range opts {
		switch v := o.(type) {
		case veyron2.RuntimeIDOpt:
			rt.id = v.PrivateID
		case veyron2.RuntimePublicIDStoreOpt:
			rt.store = v
		case veyron2.ProductOpt:
			rt.product = v.T
		case veyron2.NamespaceRoots:
			nsRoots = v
		case veyron2.HTTPDebugOpt:
			rt.debug.addr = string(v)
		default:
			return fmt.Errorf("option has wrong type %T", o)
		}
	}

	rt.initLogging()
	rt.initSignalHandling()

	if rt.Product() == nil {
		product, err := product.DetermineProduct()
		if err != nil {
			return err
		}
		rt.product = product
	}

	if len(nsRoots) == 0 {
		found := false
		for _, ev := range os.Environ() {
			p := strings.SplitN(ev, "=", 2)
			if len(p) != 2 {
				continue
			}
			k, v := p[0], p[1]
			if strings.HasPrefix(k, "NAMESPACE_ROOT") {
				nsRoots = append(nsRoots, v)
				found = true
			}
		}
		if !found {
			// TODO(cnicolaou,caprita): remove this when NAMESPACE_ROOT is in use.
			if nsRoot := os.Getenv("MOUNTTABLE_ROOT"); nsRoot != "" {
				nsRoots = append(nsRoots, nsRoot)
			}
		}
	}

	if ns, err := namespace.New(rt, nsRoots...); err != nil {
		return fmt.Errorf("Couldn't create mount table: %v", err)
	} else {
		rt.ns = ns
	}
	vlog.VI(2).Infof("Namespace Roots: %s", nsRoots)

	var err error
	if rt.sm, err = rt.NewStreamManager(); err != nil {
		return err
	}

	if err = rt.initSecurity(); err != nil {
		return err
	}

	if rt.client, err = rt.NewClient(); err != nil {
		return err
	}

	handle, err := exec.GetChildHandle()
	switch err {
	case exec.ErrNoVersion:
		// The process has not been started through the veyron exec
		// library. No further action is needed.
	case nil:
		// The process has been started through the veyron exec
		// library. Signal the parent the the child is ready.
		handle.SetReady()
	default:
		return err
	}

	if err := rt.mgmt.init(rt); err != nil {
		return err
	}

	vlog.VI(2).Infof("rt.Init done")
	return nil
}

func (rt *vrt) Cleanup() {
	// TODO(caprita): Consider shutting down mgmt later in the runtime's
	// shutdown sequence, to capture some of the runtime internal shutdown
	// tasks in the task tracker.
	rt.mgmt.shutdown()
	rt.sm.Shutdown()
	rt.shutdownSignalHandling()
	rt.shutdownLogging()
}
