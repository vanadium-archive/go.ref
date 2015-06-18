// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"v.io/x/lib/cmdline"

	"v.io/v23/context"

	"v.io/x/ref/examples/tunnel"
	"v.io/x/ref/examples/tunnel/internal"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
)

var (
	disablePty, forcePty, noshell     bool
	portforward, lprotocol, rprotocol string
)

func main() {
	cmdVsh.Flags.BoolVar(&disablePty, "T", false, "Disable pseudo-terminal allocation.")
	cmdVsh.Flags.BoolVar(&forcePty, "t", false, "Force allocation of pseudo-terminal.")
	cmdVsh.Flags.BoolVar(&noshell, "N", false, "Do not execute a shell.  Only do port forwarding.")
	cmdVsh.Flags.StringVar(&portforward, "L", "", `Forward local to remote, format is "localaddr,remoteaddr".`)
	cmdVsh.Flags.StringVar(&lprotocol, "local_protocol", "tcp", "Local network protocol for port forwarding.")
	cmdVsh.Flags.StringVar(&rprotocol, "remote_protocol", "tcp", "Remote network protocol for port forwarding.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdVsh)
}

var cmdVsh = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runVsh),
	Name:   "vsh",
	Short:  "Vanadium shell",
	Long: `
Command vsh runs the Vanadium shell, a Tunnel client that can be used to run
shell commands or start an interactive shell on a remote tunneld server.

To open an interactive shell, use:
  vsh <object name>

To run a shell command, use:
  vsh <object name> <command to run>

The -L flag will forward connections from a local port to a remote address
through the tunneld service. The flag value is localaddr,remoteaddr. E.g.
  -L :14141,www.google.com:80

vsh can't be used directly with tools like rsync because vanadium object names
don't look like traditional hostnames, which rsync doesn't understand. For
compatibility with such tools, vsh has a special feature that allows passing the
vanadium object name via the VSH_NAME environment variable.

  $ VSH_NAME=<object name> rsync -avh -e vsh /foo/* v23:/foo/

In this example, the "v23" host will be substituted with $VSH_NAME by vsh and
rsync will work as expected.
`,
	ArgsName: "<object name> [command]",
	ArgsLong: `
<object name> is the Vanadium object name to connect to.

[command] is the shell command and args to run, for non-interactive vsh.
`,
}

func runVsh(ctx *context.T, env *cmdline.Env, args []string) error {
	oname, cmd, err := objectNameAndCommandLine(env, args)
	if err != nil {
		return env.UsageErrorf("%v", err)
	}

	t := tunnel.TunnelClient(oname)

	if len(portforward) > 0 {
		go runPortForwarding(ctx, t, oname)
	}

	if noshell {
		<-signals.ShutdownOnSignals(ctx)
		return nil
	}

	opts := shellOptions(env, cmd)

	stream, err := t.Shell(ctx, cmd, opts)
	if err != nil {
		return err
	}
	if opts.UsePty {
		saved := internal.EnterRawTerminalMode()
		defer internal.RestoreTerminalSettings(saved)
	}
	runIOManager(env.Stdin, env.Stdout, env.Stderr, stream)

	exitMsg := fmt.Sprintf("Connection to %s closed.", oname)
	exitStatus, err := stream.Finish()
	if err != nil {
		exitMsg += fmt.Sprintf(" (%v)", err)
	}
	ctx.VI(1).Info(exitMsg)
	// Only show the exit message on stdout for interactive shells.
	// Otherwise, the exit message might get confused with the output
	// of the command that was run.
	if err != nil {
		fmt.Fprintln(env.Stderr, exitMsg)
	} else if len(cmd) == 0 {
		fmt.Println(exitMsg)
	}
	return cmdline.ErrExitCode(exitStatus)
}

func shellOptions(env *cmdline.Env, cmd string) (opts tunnel.ShellOpts) {
	opts.UsePty = (len(cmd) == 0 || forcePty) && !disablePty
	opts.Environment = environment(env.Vars)
	ws, err := internal.GetWindowSize()
	if err != nil {
		logger.Global().VI(1).Infof("GetWindowSize failed: %v", err)
	} else {
		opts.WinSize.Rows = ws.Row
		opts.WinSize.Cols = ws.Col
	}
	return
}

func environment(vars map[string]string) []string {
	env := []string{}
	for _, name := range []string{"TERM", "COLORTERM"} {
		if value := vars[name]; value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// objectNameAndCommandLine extracts the object name and the remote command to
// send to the server. The object name is the first non-flag argument.
// The command line is the concatenation of all non-flag arguments excluding
// the object name.
func objectNameAndCommandLine(env *cmdline.Env, args []string) (string, string, error) {
	if len(args) < 1 {
		return "", "", errors.New("object name missing")
	}
	name := args[0]
	args = args[1:]
	// For compatibility with tools like rsync. Because object names
	// don't look like traditional hostnames, tools that work with rsh and
	// ssh can't work directly with vsh. This trick makes the following
	// possible:
	//   $ VSH_NAME=<object name> rsync -avh -e vsh /foo/* v23:/foo/
	// The "v23" host will be substituted with <object name>.
	if envName := env.Vars["VSH_NAME"]; envName != "" && name == "v23" {
		name = envName
	}
	cmd := strings.Join(args, " ")
	return name, cmd, nil
}

func runPortForwarding(ctx *context.T, t tunnel.TunnelClientMethods, oname string) {
	// portforward is localaddr,remoteaddr
	parts := strings.Split(portforward, ",")
	var laddr, raddr string
	if len(parts) != 2 {
		ctx.Fatalf("-L flag expects 2 values separated by a comma")
	}
	laddr = parts[0]
	raddr = parts[1]

	ln, err := net.Listen(lprotocol, laddr)
	if err != nil {
		ctx.Fatalf("net.Listen(%q, %q) failed: %v", lprotocol, laddr, err)
	}
	defer ln.Close()
	ctx.VI(1).Infof("Listening on %q", ln.Addr())
	for {
		conn, err := ln.Accept()
		if err != nil {
			ctx.Infof("Accept failed: %v", err)
			continue
		}
		stream, err := t.Forward(ctx, rprotocol, raddr)
		if err != nil {
			ctx.Infof("Tunnel(%q, %q) failed: %v", rprotocol, raddr, err)
			conn.Close()
			continue
		}
		name := fmt.Sprintf("%v-->%v-->(%v)-->%v", conn.RemoteAddr(), conn.LocalAddr(), oname, raddr)
		go func() {
			ctx.VI(1).Infof("TUNNEL START: %v", name)
			errf := internal.Forward(conn, stream.SendStream(), stream.RecvStream())
			err := stream.Finish()
			ctx.VI(1).Infof("TUNNEL END  : %v (%v, %v)", name, errf, err)
		}()
	}
}
