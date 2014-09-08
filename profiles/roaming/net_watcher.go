// +build ignore

package main

import (
	"fmt"

	"veyron2/config"
	"veyron2/rt"

	"veyron/profiles/roaming"
)

func main() {
	r := rt.Init()
	defer r.Cleanup()

	fmt.Println("Profile: ", r.Profile().Name())

	if addrOpt := r.Profile().PreferredAddressOpt(); addrOpt != nil {
		if gce, err := addrOpt("", nil); err == nil {
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
