package rt

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/runtimes/google/naming/namespace"
)

// TODO(caprita): Verrorize this, and maybe move it in the API.
var errCleaningUp = fmt.Errorf("operation rejected: runtime is being cleaned up")

type vrt struct {
	mu sync.Mutex

	profile    veyron2.Profile
	publisher  *config.Publisher
	sm         []stream.Manager // GUARDED_BY(mu)
	ns         naming.Namespace
	signals    chan os.Signal
	id         security.PrivateID
	store      security.PublicIDStore
	client     ipc.Client
	mgmt       *mgmtImpl
	debug      debugServer
	nServers   int  // GUARDED_BY(mu)
	cleaningUp bool // GUARDED_BY(mu)
}

// Implements veyron2/rt.New
func New(opts ...veyron2.ROpt) (veyron2.Runtime, error) {
	rt := &vrt{mgmt: new(mgmtImpl)}
	flag.Parse()
	rt.initHTTPDebugServer()
	nsRoots := []string{}
	for _, o := range opts {
		switch v := o.(type) {
		case veyron2.RuntimeIDOpt:
			rt.id = v.PrivateID
		case veyron2.RuntimePublicIDStoreOpt:
			rt.store = v
		case veyron2.ProfileOpt:
			rt.profile = v.Profile
		case veyron2.NamespaceRoots:
			nsRoots = v
		case veyron2.HTTPDebugOpt:
			rt.debug.addr = string(v)
		case veyron2.RuntimeOpt:
			if v.Name != "google" && v.Name != "" {
				return nil, fmt.Errorf("%q is the wrong name for this runtime", v.Name)
			}
		default:
			return nil, fmt.Errorf("option has wrong type %T", o)
		}
	}

	rt.initLogging()
	rt.initSignalHandling()

	if rt.profile == nil {
		rt.profile = profiles.New()
	}
	vlog.VI(1).Infof("Using profile %q", rt.profile.Name())

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
		return nil, fmt.Errorf("Couldn't create mount table: %v", err)
	} else {
		rt.ns = ns
	}
	vlog.VI(2).Infof("Namespace Roots: %s", nsRoots)

	// Create the default stream manager.
	if _, err := rt.NewStreamManager(); err != nil {
		return nil, err
	}

	if err := rt.initSecurity(); err != nil {
		return nil, err
	}

	var err error
	if rt.client, err = rt.NewClient(); err != nil {
		return nil, err
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
		return nil, err
	}

	if err := rt.mgmt.init(rt); err != nil {
		return nil, err
	}

	rt.publisher = config.NewPublisher()
	if err := rt.profile.Init(rt, rt.publisher); err != nil {
		return nil, err
	}

	vlog.VI(2).Infof("rt.Init done")
	return rt, nil
}

func (rt *vrt) Publisher() *config.Publisher {
	return rt.publisher
}

func (r *vrt) Profile() veyron2.Profile {
	return r.profile
}

func (rt *vrt) Cleanup() {
	rt.mu.Lock()
	if rt.cleaningUp {
		rt.mu.Unlock()
		// TODO(caprita): Should we actually treat extra Cleanups as
		// silent no-ops?  For now, it's better not to, in order to
		// expose programming errors.
		vlog.Errorf("rt.Cleanup done")
		return
	}
	rt.cleaningUp = true
	rt.mu.Unlock()

	// TODO(caprita): Consider shutting down mgmt later in the runtime's
	// shutdown sequence, to capture some of the runtime internal shutdown
	// tasks in the task tracker.
	rt.mgmt.shutdown()
	// It's ok to access rt.sm out of lock below, since a Mutex acts as a
	// barrier in Go and hence we're guaranteed that cleaningUp is true at
	// this point.  The only code that mutates rt.sm is NewStreamManager in
	// ipc.go, which respects the value of cleaningUp.
	//
	// TODO(caprita): It would be cleaner if we do something like this
	// inside the lock (and then iterate over smgrs below):
	//   smgrs := rt.sm
	//   rt.sm = nil
	// However, to make that work we need to prevent NewClient and NewServer
	// from using rt.sm after cleaningUp has been set.  Hence the TODO.
	for _, sm := range rt.sm {
		sm.Shutdown()
	}
	rt.shutdownSignalHandling()
	rt.shutdownLogging()
}
