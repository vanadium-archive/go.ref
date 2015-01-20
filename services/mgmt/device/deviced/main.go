package main

import "v.io/lib/cmdline"

func main() {
	rootCmd := cmdline.Command{
		Name:  "deviced",
		Short: "Veyron device manager setup",
		Long: `
deviced can be used to launch, configure, or manage the device manager.
`,
		Children: []*cmdline.Command{cmdInstall, cmdUninstall, cmdStart, cmdStop, cmdProfile},
		Run:      runServer,
	}
	rootCmd.Main()
}
