// Package core provides modules.Shell instances with commands preinstalled for
// common core services such as naming, security etc.
//
// The available commands are:
//
// root     - runs a root mount table as a subprocess
//            prints the MT_NAME=<root name>, PID=<pid> variables to stdout
//            waits for stdin to be closed before it exits
//            prints "done" to stdout when stdin is closed.
// mt <mp>  - runs a mount table as a subprocess mounted on <mp>
//            NAMESPACE_ROOT should be set to the name of another mount table
//            (e.g. the value of MT_NAME from a root) in the shell's
//            environment. mt similarly prints MT_NAME, PID and waits for stdout
//            as per the root
//            command
//
// ls <glob>...  - ls issues one or more globs using the local,
//                 in-process namespace
// lse <glob>... - lse issues one or more globs from a subprocess and hence
//                 the subprocesses namespace. NAMESPACE_ROOT can be set in
//                 the shell's environment prior to calling lse to have the
//                 subprocesses namespace be relative to it.
//
package core

import "veyron.io/veyron/veyron/lib/modules"

const (
	RootMTCommand     = "root"
	MTCommand         = "mt"
	LSCommand         = "ls"
	LSExternalCommand = "lse"
	// TODO(cnicolaou): provide a simple service that can be mounted.
	MountCommand = "mount"
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
