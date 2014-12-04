// +build ignore

package main

import (
	"fmt"
	"strings"

	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/profiles/roaming"
)

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer r.Cleanup()

	fmt.Println("Profile: ", r.Profile().Name())

	accessible, err := netstate.GetAccessibleIPs()
	routes := netstate.GetRoutes()
	fmt.Printf("Routes:\n%s\n", strings.Replace(routes.String(), ")", ")\n", -1))

	chooser := roaming.ListenSpec.AddressChooser
	if chooser != nil {
		if gce, err := chooser("", nil); err == nil {
			fmt.Printf("%s: 1:1 NAT address is %s\n", r.Profile().Name(), gce)
		}
	}

	if chosen, err := roaming.ListenSpec.AddressChooser("tcp", accessible); err != nil {
		fmt.Printf("Failed to chosen address %s\n", err)
	} else {
		al := netstate.AddrList(chosen)
		fmt.Printf("Chosen:\n%s\n", strings.Replace(al.String(), ") ", ")\n", -1))
	}

	ch := make(chan config.Setting, 10)
	settings, err := r.Publisher().ForkStream(roaming.SettingsStreamName, ch)
	if err != nil {
		r.Logger().Infof("failed to fork stream: %s", err)
	}
	for _, setting := range settings.Latest {
		fmt.Println("Setting: ", setting)
	}
	for setting := range ch {
		fmt.Println("Setting: ", setting)
	}
}
