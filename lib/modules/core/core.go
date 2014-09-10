// Package core provides modules.Shell instances with commands preinstalled for
// common core services such as naming, security etc.
package core

import "veyron/lib/modules"

const (
	RootMTCommand     = "root"
	MTCommand         = "mt"
	LSCommand         = "ls"
	LSExternalCommand = "lse"
	MountCommand      = "mount"
)

func NewShell() *modules.Shell {
	shell := modules.NewShell()
	shell.AddSubprocess(RootMTCommand, "")
	shell.AddSubprocess(MTCommand, `<mount point>
	reads NAMESPACE_ROOT from its environment and mounts a new mount table at <mount point>`)
	shell.AddFunction(LSCommand, ls, `<glob>...
	issues glob requests using the current processes namespace library`)
	shell.AddSubprocess(LSExternalCommand, `<glob>...
	runs a subprocess to issue glob requests using the subprocesses namespace library`)
	return shell
}
