package rt

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"veyron/lib/exec"
	"veyron/runtimes/google/naming/namespace"

	"veyron2"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/product"
	"veyron2/vlog"
)

type vrt struct {
	product    product.T
	httpServer string
	sm         stream.Manager
	ns         naming.Namespace
	rid        naming.RoutingID
	signals    chan os.Signal
	id         *currentIDOpt
	client     ipc.Client
	mgmt       *mgmtImpl
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
	// TODO(ashankar,cnicolaou): Change the default debug address to
	// the empty string.
	// In March 2014 this was temporarily set to "127.0.0.1:0" so that the debugging
	// HTTP server always runs, which was useful during initial veyron
	// development. We restrict it to localhost to avoid annoying firewall
	// warnings and to provide a modicum of security.
	rt.httpServer = "127.0.0.1:0"
	rt.id = &currentIDOpt{}
	nsRoots := []string{}

	for _, o := range opts {
		switch v := o.(type) {
		case veyron2.LocalIDOpt:
			rt.id.setIdentity(v.PrivateID)
		case veyron2.ProductOpt:
			rt.product = v.T
		case veyron2.NamespaceRoots:
			nsRoots = v
		case veyron2.HTTPDebugOpt:
			rt.httpServer = string(v)
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
		// TODO(cnicolaou,caprita): remove this when NAMESPACE_ROOT is in use.
		if nsRoot := os.Getenv("MOUNTTABLE_ROOT"); nsRoot != "" {
			nsRoots = append(nsRoots, nsRoot)
		}
		// TODO(cnicolaou,caprita): rename this to NAMESPACE_ROOT.
		if nsRoot := os.Getenv("NAMESPACE_ROOT"); nsRoot != "" {
			nsRoots = append(nsRoots, nsRoot)
		}
		// TODO(cnicolaou,caprita): decide if want to allow NAMESPACE_ROOTS
		// e.g. comma separated list of roots.
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

	if err = rt.initIdentity(); err != nil {
		return err
	}

	if rt.client, err = rt.NewClient(veyron2.LocalID(rt.id.Identity())); err != nil {
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

	vlog.VI(2).Infof("rt.Init done")
	return nil
}

func (rt *vrt) Shutdown() {
	rt.sm.Shutdown()
	rt.shutdownSignalHandling()
	rt.shutdownLogging()
}
