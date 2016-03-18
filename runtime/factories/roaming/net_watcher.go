// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"fmt"
	"os"
	"strings"

	"v.io/v23"
	"v.io/v23/config"
	"v.io/x/lib/netstate"
	"v.io/x/ref/runtime/factories/roaming"
)

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

	profileName := "roaming"
	fmt.Println("Profile: ", profileName)

	accessible, err := netstate.GetAccessibleIPs()
	interfaces, err := netstate.GetAllInterfaces()

	fmt.Printf("Addresses\n")
	for _, addr := range accessible {
		fmt.Printf("%s\n", addr.DebugString())
	}

	fmt.Printf("\nInterfaces\n")
	for _, ifc := range interfaces {
		fmt.Printf("%s\n", ifc)
	}

	fmt.Printf("\nRoutes\n")
	for _, ifc := range interfaces {
		if ipifc, ok := ifc.(netstate.IPNetworkInterface); ok {
			if routes := ipifc.IPRoutes(); len(routes) > 0 {
				fmt.Printf("%s: %s\n", ifc.Name(), routes)
			}
		}
	}

	listenSpec := v23.GetListenSpec(ctx)
	chooser := listenSpec.AddressChooser
	if chooser != nil {
		if gce, err := chooser("", nil); err == nil {
			fmt.Printf("%s: 1:1 NAT address is %s\n", profileName, gce)
		}
	}

	if chosen, err := listenSpec.AddressChooser("tcp", accessible.AsNetAddrs()); err != nil {
		fmt.Printf("Failed to chosen address %s\n", err)
	} else {
		al := netstate.ConvertToAddresses(chosen)
		fmt.Printf("Chosen:\n%s\n", strings.Replace(al.String(), ") ", ")\n", -1))
	}

	ch := make(chan config.Setting, 10)
	settings, err := listenSpec.StreamPublisher.ForkStream(roaming.SettingsStreamName, ch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fork stream: %s\n", err)
	}
	for _, setting := range settings.Latest {
		fmt.Println("Setting: ", setting)
	}
	for setting := range ch {
		fmt.Println("Setting: ", setting)
	}
}
