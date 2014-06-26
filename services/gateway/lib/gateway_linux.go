package gateway

import (
	"fmt"

	"veyron/lib/bluetooth"
	"veyron2/services/proximity"
	"veyron2/vlog"
)

var gatewayCmd = [][]string{
	{"route"},
	// TODO(spetrovic): Use Go's string libraries to reduce
	// dependence on these tools.
	{"grep", "UG[ \t]"},
	{"grep", "^default"},
	{"head", "-1"},
	{"awk", `{printf "%s", $NF}`},
}

// New creates a new instance of the gateway service given the name of the
// local proximity service.  Since the gateway server operates in two modes
// (i.e., server and client) depending on whether it provides or obtains
// internet connectivity, the provided boolean option lets us force a client
// mode, which can be useful for testing.
func New(proximityService string, forceClient bool) (*Service, error) {
	// See if we have bluetooth on this device.
	d, err := bluetooth.OpenFirstAvailableDevice()
	if err != nil {
		return nil, fmt.Errorf("no bluetooth devices found: %v", err)
	}

	// Find the default gateway interface.
	gateway, err := runPipedCmd(gatewayCmd)
	if err != nil {
		gateway = ""
	}

	p, err := proximity.BindProximity(proximityService)
	if err != nil {
		return nil, fmt.Errorf("error connecting to proximity service %q: %v", proximityService, err)
	}

	// If gateway is present, start the server; otherwise, start the client.
	s := new(Service)
	if gateway == "" || forceClient {
		vlog.Info("No IP interfaces detected: starting the gateway client.")
		var err error
		if s.client, err = newClient(p); err != nil {
			return nil, fmt.Errorf("couldn't start gateway client: %v", err)
		}
	} else {
		vlog.Infof("IP interface %q detected: starting the gateway server.", gateway)
		if s.server, err = newServer(p, gateway, d.Name); err != nil {
			return nil, fmt.Errorf("couldn't start gateway server: %v", err)
		}
	}
	return s, nil
}

type Service struct {
	client *client
	server *server
}

// Stop stops the gateway service.
func (s *Service) Stop() {
	if s.client != nil {
		s.client.Stop()
	}
	if s.server != nil {
		s.server.Stop()
	}
}
