package profiles

import (
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/appcycle"
	_ "veyron.io/veyron/veyron/lib/tcp"
	_ "veyron.io/veyron/veyron/lib/websocket"
	"veyron.io/veyron/veyron/profiles/internal"
	"veyron.io/veyron/veyron/profiles/internal/platform"
	_ "veyron.io/veyron/veyron/runtimes/google/rt"
	"veyron.io/veyron/veyron/services/mgmt/debug"

	// TODO(cnicolaou,ashankar): move this into flags.
	sflag "veyron.io/veyron/veyron/security/flag"
)

// LocalListenSpec is a ListenSpec for 127.0.0.1.
var LocalListenSpec = ipc.ListenSpec{
	Protocol:       "tcp",
	Address:        "127.0.0.1:0",
	AddressChooser: internal.IPAddressChooser,
}

type generic struct{ ac *appcycle.AppCycle }

var _ veyron2.Profile = (*generic)(nil)

func init() {
	rt.RegisterProfile(New())
}

// New returns a new instance of a very generic Profile. It can be used
// as a default by Runtime implementations, in unit tests etc.
func New() veyron2.Profile {
	return &generic{}
}

func (*generic) Name() string {
	return "generic"
}

func (*generic) Runtime() (string, []veyron2.ROpt) {
	return veyron2.GoogleRuntimeName, nil
}

func (*generic) Platform() *veyron2.Platform {
	pstr, _ := platform.Platform()
	return pstr
}

func (g *generic) Init(rt veyron2.Runtime, _ *config.Publisher) (veyron2.AppCycle, error) {
	log := rt.Logger()
	log.VI(1).Infof("%s", g)
	g.ac = appcycle.New()

	rt.ConfigureReservedName(debug.NewDispatcher(log.LogDir(), sflag.NewAuthorizerOrDie(), rt.VtraceStore()))
	return g.ac, nil
}

func (g *generic) Cleanup() {
	g.ac.Shutdown()
}

func (g *generic) String() string {
	return "generic profile on " + g.Platform().String()
}
