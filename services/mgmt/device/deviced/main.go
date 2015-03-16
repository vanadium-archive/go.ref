package main

import (
	"os"
	"runtime"

	"v.io/x/lib/cmdline"
)

func main() {
	// TODO(caprita): Remove this once we have a way to set the GOMAXPROCS
	// environment variable persistently for device manager.
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	rootCmd := cmdline.Command{
		Name:  "deviced",
		Short: "Vanadium device manager setup",
		Long: `
deviced can be used to launch, configure, or manage the device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdUninstall, cmdStart, cmdStop, cmdProfile},
		Run:      runServer,
	}
	os.Exit(rootCmd.Main())
}
