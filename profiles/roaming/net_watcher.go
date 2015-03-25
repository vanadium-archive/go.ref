// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"fmt"
	"strings"

	"v.io/v23"
	"v.io/v23/config"

	"v.io/x/lib/netstate"
	"v.io/x/ref/profiles/roaming"
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	profileName := "roaming"
	fmt.Println("Profile: ", profileName)

	accessible, err := netstate.GetAccessibleIPs()
	routes := netstate.GetRoutes()
	fmt.Printf("Routes:\n%s\n", strings.Replace(routes.String(), ")", ")\n", -1))

	listenSpec := v23.GetListenSpec(ctx)
	chooser := listenSpec.AddressChooser
	if chooser != nil {
		if gce, err := chooser("", nil); err == nil {
			fmt.Printf("%s: 1:1 NAT address is %s\n", profileName, gce)
		}
	}

	if chosen, err := listenSpec.AddressChooser("tcp", accessible); err != nil {
		fmt.Printf("Failed to chosen address %s\n", err)
	} else {
		al := netstate.AddrList(chosen)
		fmt.Printf("Chosen:\n%s\n", strings.Replace(al.String(), ") ", ")\n", -1))
	}

	ch := make(chan config.Setting, 10)
	settings, err := v23.GetPublisher(ctx).ForkStream(roaming.SettingsStreamName, ch)
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
