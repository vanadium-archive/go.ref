// +build ignore

package main

import (
	"flag"
	"fmt"
	"os"

	"v.io/veyron/veyron/lib/flags"
)

func main() {
	fl := flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.ACL, flags.Listen)
	flag.PrintDefaults()
	fmt.Printf("Args: %v\n", os.Args)
	if err := fl.Parse(os.Args[1:]); err != nil {
		fmt.Println("ERROR: %s", err)
		return
	}
	rtf := fl.RuntimeFlags()
	fmt.Printf("Runtime: Credentials: %s\n", rtf.Credentials)
	fmt.Printf("Runtime: Namespace Roots: %s\n", rtf.NamespaceRoots)
	lf := fl.ListenFlags()
	for _, a := range lf.ListenAddrs {
		fmt.Printf("Listen: Protocol %q, Address %q\n", a.Protocol, a.Address)
	}
	fmt.Printf("Listen: Proxy %q\n", lf.ListenProxy)
	fmt.Printf("ACL: %v\n", fl.ACLFlags())
}
