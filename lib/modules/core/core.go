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
//
// echoServer <message> <name>
//    runs on echoServer at <name>, it will echo back <message>: <text>
//    where <text> is supplied by the client
// echoClient <name> <text>
//    invoke <name>.Echo(<text>)
//
// proxyd <names>...
//    runs a proxy server
package core

const (
	// Functions
	SleepCommand = "sleep"
	TimeCommand  = "time"
	// Subprocesses
	EchoServerCommand  = "echoServer"
	EchoClientCommand  = "echoClient"
	RootMTCommand      = "root"
	MTCommand          = "mt"
	LSCommand          = "ls"
	ProxyServerCommand = "proxyd"
	WSPRCommand        = "wsprd"
	ShellCommand       = "sh"
)
