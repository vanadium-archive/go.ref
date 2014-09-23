// Package core provides modules.Shell instances with commands preinstalled for
// common core services such as naming, security etc.
//
// The available commands are:
//
// root
//   runs a root mount table as a subprocess
//   prints the MT_NAME=<root name>, PID=<pid> variables to stdout
//   waits for stdin to be closed before it exits
//   prints "done" to stdout when stdin is closed.
// mt <mp>
//   runs a mount table as a subprocess mounted on <mp>
//   NAMESPACE_ROOT should be set to the name of another mount table
//   (e.g. the value of MT_NAME from a root) in the shell's environment.
//   mt similarly prints MT_NAME, PID and waits for stdout as per the root
//   command
//
// ls <glob>...
//   ls issues one or more globs using the local in-process namespace. It
//   writes: RN=<number of items> and then R0=<name> to R(N-1)=<name>
//   lines of output. If -l is specified, then each line includes a trailing
//   set of detailed information, enclosed in [].
//
// lse <glob>...
//   lse issues one or more globs from a subprocess and hence the
//   subprocesses namespace. NAMESPACE_ROOT can be set in the shell's
//   environment prior to calling lse to have the subprocesses namespace
//   be relative to it. The output format is the same ls.
//
// resolve <name>...
//    resolves name using the in-process namespace, the results are written
//    to stdout as variables R<n>=<addr>
// resolveMT <name>...
//    resolves name to obtain the mount table hosting it using
//    the in-process namespace. The results are written as R<n>=<addr>
//    as per resolve.
//
// setRoots <name>...
//    sets the local namespace's roots to the supplied names.
// echoClient
// echoServer
package core

import "veyron.io/veyron/veyron/lib/modules"

const (
	RootMTCommand            = "root"
	MTCommand                = "mt"
	LSCommand                = "ls"
	LSExternalCommand        = "lse"
	SetNamespaceRootsCommand = "setRoots"
	ResolveCommand           = "resolve"
	ResolveMTCommand         = "resolveMT"
	EchoServerCommand        = "echoServer"
	EchoClientCommand        = "echoClient"
	SleepCommand             = "sleep"
	TimeCommand              = "time"
	MountCommand             = "mount"
	NamespaceCacheCommand    = "cache"
)

// NewShell returns a new Shell instance with the core commands installed.
func NewShell() *modules.Shell {
	shell := modules.NewShell()
	Install(shell)
	return shell
}

// Install installs the core commands into the supplied Shell.
func Install(shell *modules.Shell) {
	shell.AddSubprocess(RootMTCommand, "")
	shell.AddSubprocess(MTCommand, `<mount point>
	reads NAMESPACE_ROOT from its environment and mounts a new mount table at <mount point>`)
	shell.AddFunction(LSCommand, ls, `<glob>...
	issues glob requests using the current processes namespace library`)
	shell.AddSubprocess(LSExternalCommand, `<glob>...
	runs a subprocess to issue glob requests using the subprocesses namespace library`)
	shell.AddFunction(ResolveCommand, resolveObject, `<name>
	resolves name to obtain an object server address`)
	shell.AddFunction(ResolveMTCommand, resolveMT, `<name>
	resolves name to obtain a mount table address`)
	shell.AddFunction(SetNamespaceRootsCommand, setNamespaceRoots, `<name>...
	set the in-process namespace roots to <name>...`)
	shell.AddFunction(SleepCommand, sleep, `[duration]
	sleep for a time (in go time.Duration format): defaults to 1s`)
	shell.AddFunction(TimeCommand, now, `
	prints the current time`)
	shell.AddFunction(NamespaceCacheCommand, namespaceCache, `on|off
	turns the namespace cache on or off`)
	shell.AddFunction(MountCommand, mountServer, `<mountpoint> <server> <ttl>
	invokes namespace.Mount(<mountpoint>, <server>, <ttl>)`)
	shell.AddSubprocess(EchoClientCommand, `<name> <message>...
	invokes name.Echo(message)`)
	shell.AddSubprocess(EchoServerCommand, `<name> <text>
	runs an Echo server mounted at <name> and configured to return <text>: as a prefix in its response`)
}
