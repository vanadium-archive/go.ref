package rt

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"v.io/core/veyron2"
	"v.io/core/veyron2/config"
	"v.io/core/veyron2/i18n"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/flags"
	_ "v.io/core/veyron/lib/stats/sysstats"
	"v.io/core/veyron/runtimes/google/naming/namespace"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"
)

// TODO(caprita): Verrorize this, and maybe move it in the API.
var errCleaningUp = fmt.Errorf("operation rejected: runtime is being cleaned up")

type vrt struct {
	mu                 sync.Mutex
	profile            veyron2.Profile
	publisher          *config.Publisher
	sm                 []stream.Manager // GUARDED_BY(mu)
	ns                 naming.Namespace
	signals            chan os.Signal
	principal          security.Principal
	client             ipc.Client
	ac                 veyron2.AppCycle
	acServer           ipc.Server
	flags              flags.RuntimeFlags
	preferredProtocols options.PreferredProtocols
	reservedDisp       ipc.Dispatcher
	reservedOpts       []ipc.ServerOpt
	nServers           int  // GUARDED_BY(mu)
	cleaningUp         bool // GUARDED_BY(mu)

	lang       i18n.LangID    // Language, from environment variables.
	program    string         // Program name, from os.Args[0].
	traceStore *ivtrace.Store // Storage of sampled vtrace traces.
}

var _ veyron2.Runtime = (*vrt)(nil)

var flagsOnce sync.Once
var runtimeFlags *flags.Flags

func init() {
	rt.RegisterRuntime(veyron2.GoogleRuntimeName, New)
	runtimeFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
}

// Implements veyron2/rt.New
func New(opts ...veyron2.ROpt) (veyron2.Runtime, error) {
	flagsOnce.Do(func() {
		runtimeFlags.Parse(os.Args[1:])
	})
	flags := runtimeFlags.RuntimeFlags()
	rt := &vrt{
		lang:       i18n.LangIDFromEnv(),
		program:    filepath.Base(os.Args[0]),
		flags:      flags,
		traceStore: ivtrace.NewStore(flags.Vtrace),
	}

	for _, o := range opts {
		switch v := o.(type) {
		case options.RuntimePrincipal:
			rt.principal = v.Principal
		case options.Profile:
			rt.profile = v.Profile
		case options.PreferredProtocols:
			rt.preferredProtocols = v
		default:
			return nil, fmt.Errorf("option has wrong type %T", o)
		}
	}

	rt.initLogging()
	rt.initSignalHandling()

	if rt.profile == nil {
		return nil, fmt.Errorf("No profile configured!")
	} else {
		vlog.VI(1).Infof("Using profile %q", rt.profile.Name())
	}

	if ns, err := namespace.New(rt.flags.NamespaceRoots...); err != nil {
		return nil, fmt.Errorf("Couldn't create mount table: %v", err)
	} else {
		rt.ns = ns
	}
	vlog.VI(2).Infof("Namespace Roots: %s", rt.ns.Roots())

	// Create the default stream manager.
	if _, err := rt.NewStreamManager(); err != nil {
		return nil, fmt.Errorf("failed to create stream manager: %s", err)
	}

	handle, err := exec.GetChildHandle()
	switch err {
	case exec.ErrNoVersion:
		// The process has not been started through the veyron exec
		// library. No further action is needed.
	case nil:
		// The process has been started through the veyron exec
		// library.
	default:
		return nil, fmt.Errorf("failed to get child handle: %s", err)
	}

	if err := rt.initSecurity(handle, rt.flags.Credentials); err != nil {
		return nil, fmt.Errorf("failed to init sercurity: %s", err)
	}

	if len(rt.flags.I18nCatalogue) != 0 {
		cat := i18n.Cat()
		for _, filename := range strings.Split(rt.flags.I18nCatalogue, ",") {
			err := cat.MergeFromFile(filename)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: i18n: error reading i18n catalogue file %q: %s\n", os.Args[0], filename, err)
			}
		}
	}

	if rt.client, err = rt.NewClient(); err != nil {
		return nil, fmt.Errorf("failed to create new client: %s", err)
	}

	if handle != nil {
		// Signal the parent the the child is ready.
		handle.SetReady()
	}

	rt.publisher = config.NewPublisher()
	if rt.ac, err = rt.profile.Init(rt, rt.publisher); err != nil {
		return nil, err
	}
	server, err := rt.initMgmt(rt.ac, handle)
	if err != nil {
		return nil, err
	}
	rt.acServer = server
	vlog.VI(2).Infof("rt.Init done")
	return rt, nil
}

func (rt *vrt) Publisher() *config.Publisher {
	return rt.publisher
}

func (rt *vrt) Profile() veyron2.Profile {
	return rt.profile
}

func (rt *vrt) ConfigureReservedName(server ipc.Dispatcher, opts ...ipc.ServerOpt) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reservedDisp = server
	rt.reservedOpts = make([]ipc.ServerOpt, 0, len(opts))
	copy(rt.reservedOpts, opts)
}

func (rt *vrt) VtraceStore() vtrace.Store {
	return rt.traceStore
}

func (rt *vrt) Cleanup() {
	if rt.flags.Vtrace.DumpOnShutdown {
		vtrace.FormatTraces(os.Stderr, rt.traceStore.TraceRecords(), nil)
	}

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
	rt.profile.Cleanup()
	if rt.acServer != nil {
		rt.acServer.Stop()
	}

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
