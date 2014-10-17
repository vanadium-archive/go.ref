package profiles

import (
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/profiles/internal"
	_ "veyron.io/veyron/veyron/runtimes/google/rt"
)

// LocalListenSpec is a ListenSpec for 127.0.0.1.
var LocalListenSpec = &ipc.ListenSpec{
	Protocol:       "tcp",
	Address:        "127.0.0.1:0",
	AddressChooser: internal.IPAddressChooser,
}

type generic struct{}

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

func (*generic) Runtime() string {
	return veyron2.GoogleRuntimeName
}

func (*generic) Platform() *veyron2.Platform {
	p, _ := Platform()
	return p
}

func (g *generic) Init(rt veyron2.Runtime, _ *config.Publisher) error {
	rt.Logger().VI(1).Infof("%s", g)
	return nil
}

func (g *generic) String() string {
	return "generic profile on " + g.Platform().String()
}
