// Command vsh is a tunnel service client that can be used to start a
// shell on the server.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"strings"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	_ "veyron.io/veyron/veyron/profiles/roaming"

	"veyron.io/apps/tunnel"
	"veyron.io/apps/tunnel/tunnelutil"
	"veyron.io/veyron/veyron/lib/signals"
)

var (
	disablePty = flag.Bool("T", false, "Disable pseudo-terminal allocation.")
	forcePty   = flag.Bool("t", false, "Force allocation of pseudo-terminal.")

	portforward = flag.String("L", "", "localaddr,remoteaddr Forward local 'localaddr' to 'remoteaddr'")
	lprotocol   = flag.String("local_protocol", "tcp", "Local network protocol for port forwarding")
	rprotocol   = flag.String("remote_protocol", "tcp", "Remote network protocol for port forwarding")

	noshell = flag.Bool("N", false, "Do not execute a shell. Only do port forwarding.")
)

func init() {
	flag.Usage = func() {
		bname := path.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, `%s: Veyron SHell.

This tool is used to run shell commands or an interactive shell on a remote
tunneld service.

To open an interactive shell, use:
  %s <object name>

To run a shell command, use:
  %s <object name> <command to run>

The -L flag will forward connections from a local port to a remote address
through the tunneld service. The flag value is localaddr,remoteaddr. E.g.
  -L :14141,www.google.com:80

%s can't be used directly with tools like rsync because veyron object names
don't look like traditional hostnames, which rsync doesn't understand. For
compatibility with such tools, %s has a special feature that allows passing the
veyron object name via the VSH_NAME environment variable.

  $ VSH_NAME=<object name> rsync -avh -e %s /foo/* veyron:/foo/

In this example, the "veyron" host will be substituted with $VSH_NAME by %s
and rsync will work as expected.

Full flags:
`, os.Args[0], bname, bname, bname, bname, os.Args[0], bname)
		flag.PrintDefaults()
	}
}

func main() {
	// Work around the fact that os.Exit doesn't run deferred functions.
	os.Exit(realMain())
}

func realMain() int {
	r := rt.Init()
	defer r.Cleanup()

	oname, cmd, err := objectNameAndCommandLine()
	if err != nil {
		flag.Usage()
		fmt.Fprintf(os.Stderr, "\n%v\n", err)
		return 1
	}

	t, err := tunnel.BindTunnel(oname)
	if err != nil {
		vlog.Fatalf("BindTunnel(%q) failed: %v", oname, err)
	}

	ctx := rt.R().NewContext()

	if len(*portforward) > 0 {
		go runPortForwarding(ctx, t, oname)
	}

	if *noshell {
		<-signals.ShutdownOnSignals()
		return 0
	}

	opts := shellOptions(cmd)

	stream, err := t.Shell(ctx, cmd, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if opts.UsePty {
		saved := tunnelutil.EnterRawTerminalMode()
		defer tunnelutil.RestoreTerminalSettings(saved)
	}
	runIOManager(os.Stdin, os.Stdout, os.Stderr, stream)

	exitMsg := fmt.Sprintf("Connection to %s closed.", oname)
	exitStatus, err := stream.Finish()
	if err != nil {
		exitMsg += fmt.Sprintf(" (%v)", err)
	}
	vlog.VI(1).Info(exitMsg)
	// Only show the exit message on stdout for interactive shells.
	// Otherwise, the exit message might get confused with the output
	// of the command that was run.
	if err != nil {
		fmt.Fprintln(os.Stderr, exitMsg)
	} else if len(cmd) == 0 {
		fmt.Println(exitMsg)
	}
	return int(exitStatus)
}

func shellOptions(cmd string) (opts tunnel.ShellOpts) {
	opts.UsePty = (len(cmd) == 0 || *forcePty) && !*disablePty
	opts.Environment = environment()
	ws, err := tunnelutil.GetWindowSize()
	if err != nil {
		vlog.VI(1).Infof("GetWindowSize failed: %v", err)
	} else {
		opts.Rows = uint32(ws.Row)
		opts.Cols = uint32(ws.Col)
	}
	return
}

func environment() []string {
	env := []string{}
	for _, name := range []string{"TERM", "COLORTERM"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, fmt.Sprintf("%s=%s", name, value))
		}
	}
	return env
}

// objectNameAndCommandLine extracts the object name and the remote command to
// send to the server. The object name is the first non-flag argument.
// The command line is the concatenation of all non-flag arguments excluding
// the object name.
func objectNameAndCommandLine() (string, string, error) {
	args := flag.Args()
	if len(args) < 1 {
		return "", "", errors.New("object name missing")
	}
	name := args[0]
	args = args[1:]
	// For compatibility with tools like rsync. Because object names
	// don't look like traditional hostnames, tools that work with rsh and
	// ssh can't work directly with vsh. This trick makes the following
	// possible:
	//   $ VSH_NAME=<object name> rsync -avh -e vsh /foo/* veyron:/foo/
	// The "veyron" host will be substituted with <object name>.
	if envName := os.Getenv("VSH_NAME"); len(envName) > 0 && name == "veyron" {
		name = envName
	}
	cmd := strings.Join(args, " ")
	return name, cmd, nil
}

func runPortForwarding(ctx context.T, t tunnel.Tunnel, oname string) {
	// *portforward is localaddr,remoteaddr
	parts := strings.Split(*portforward, ",")
	var laddr, raddr string
	if len(parts) != 2 {
		vlog.Fatalf("-L flag expects 2 values separated by a comma")
	}
	laddr = parts[0]
	raddr = parts[1]

	ln, err := net.Listen(*lprotocol, laddr)
	if err != nil {
		vlog.Fatalf("net.Listen(%q, %q) failed: %v", *lprotocol, laddr, err)
	}
	defer ln.Close()
	vlog.VI(1).Infof("Listening on %q", ln.Addr())
	for {
		conn, err := ln.Accept()
		if err != nil {
			vlog.Infof("Accept failed: %v", err)
			continue
		}
		stream, err := t.Forward(ctx, *rprotocol, raddr)
		if err != nil {
			vlog.Infof("Tunnel(%q, %q) failed: %v", *rprotocol, raddr, err)
			conn.Close()
			continue
		}
		name := fmt.Sprintf("%v-->%v-->(%v)-->%v", conn.RemoteAddr(), conn.LocalAddr(), oname, raddr)
		go func() {
			vlog.VI(1).Infof("TUNNEL START: %v", name)
			errf := tunnelutil.Forward(conn, stream.SendStream(), stream.RecvStream())
			err := stream.Finish()
			vlog.VI(1).Infof("TUNNEL END  : %v (%v, %v)", name, errf, err)
		}()
	}
}
