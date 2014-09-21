// +build ignore

package main

import (
	"fmt"

	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/profiles/roaming"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	fmt.Println("Profile: ", r.Profile().Name())

	if chooser := roaming.ListenSpec.AddressChooser; chooser != nil {
		if gce, err := chooser("", nil); err == nil {
			fmt.Printf("%s: 1:1 NAT address is %s\n", r.Profile().Name(), gce)
		}
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
