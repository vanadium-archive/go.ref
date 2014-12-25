package profiles

import (
	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/config"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/rt"

	"v.io/veyron/veyron/lib/appcycle"
	"v.io/veyron/veyron/profiles/internal"
	"v.io/veyron/veyron/profiles/internal/platform"
	_ "v.io/veyron/veyron/runtimes/google/ipc/protocols/tcp"
	_ "v.io/veyron/veyron/runtimes/google/ipc/protocols/ws"
	_ "v.io/veyron/veyron/runtimes/google/ipc/protocols/wsh"
	_ "v.io/veyron/veyron/runtimes/google/rt"
)

// LocalListenSpec is a ListenSpec for 127.0.0.1.
var LocalListenSpec = ipc.ListenSpec{
	Addrs:          ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}},
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
	rt.Logger().VI(1).Infof("%s", g)
	g.ac = appcycle.New()
	return g.ac, nil
}

func (g *generic) Cleanup() {
	g.ac.Shutdown()
}

func (g *generic) String() string {
	return "generic profile on " + g.Platform().String()
}
