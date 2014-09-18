package impl

import (
	"github.com/kr/pty"

	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"syscall"

	"veyron.io/examples/tunnel"
	"veyron.io/examples/tunnel/lib"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vlog"
)

// T implements tunnel.TunnelService
type T struct {
}

const nonShellErrorCode = 255

func (t *T) Forward(ctx ipc.ServerContext, network, address string, stream tunnel.TunnelServiceForwardStream) error {
	conn, err := net.Dial(network, address)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("RemoteID:%v LocalAddr:%v RemoteAddr:%v", ctx.RemoteID(), conn.LocalAddr(), conn.RemoteAddr())
	vlog.Infof("TUNNEL START: %v", name)
	err = lib.Forward(conn, stream.SendStream(), stream.RecvStream())
	vlog.Infof("TUNNEL END  : %v (%v)", name, err)
	return err
}

func (t *T) Shell(ctx ipc.ServerContext, command string, shellOpts tunnel.ShellOpts, stream tunnel.TunnelServiceShellStream) (int32, error) {
	vlog.Infof("SHELL START for %v: %q", ctx.RemoteID(), command)
	shell, err := findShell()
	if err != nil {
		return nonShellErrorCode, err
	}
	var c *exec.Cmd
	// An empty command means that we need an interactive shell.
	if len(command) == 0 {
		c = exec.Command(shell, "-i")
		sendMotd(stream)
	} else {
		c = exec.Command(shell, "-c", command)
	}

	c.Env = []string{
		fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
		fmt.Sprintf("VEYRON_LOCAL_IDENTITY=%s", ctx.LocalID()),
		fmt.Sprintf("VEYRON_REMOTE_IDENTITY=%s", ctx.RemoteID()),
	}
	c.Env = append(c.Env, shellOpts.Environment...)
	vlog.Infof("Shell environment: %v", c.Env)

	c.Dir = os.Getenv("HOME")
	vlog.Infof("Shell CWD: %v", c.Dir)

	var (
		stdin          io.WriteCloser // We write to stdin.
		stdout, stderr io.ReadCloser  // We read from stdout and stderr.
		ptyFd          uintptr        // File descriptor for pty.
	)

	if shellOpts.UsePty {
		f, err := pty.Start(c)
		if err != nil {
			return nonShellErrorCode, err
		}
		stdin = f
		stdout = f
		stderr = nil
		ptyFd = f.Fd()

		defer f.Close()

		setWindowSize(ptyFd, shellOpts.Rows, shellOpts.Cols)
	} else {
		var err error
		if stdin, err = c.StdinPipe(); err != nil {
			return nonShellErrorCode, err
		}
		defer stdin.Close()

		if stdout, err = c.StdoutPipe(); err != nil {
			return nonShellErrorCode, err
		}
		defer stdout.Close()

		if stderr, err = c.StderrPipe(); err != nil {
			return nonShellErrorCode, err
		}
		defer stderr.Close()

		if err = c.Start(); err != nil {
			vlog.Infof("Cmd.Start failed: %v", err)
			return nonShellErrorCode, err
		}
	}

	defer c.Process.Kill()

	select {
	case runErr := <-runIOManager(stdin, stdout, stderr, ptyFd, stream):
		vlog.Infof("SHELL END for %v: %q (%v)", ctx.RemoteID(), command, runErr)
		return harvestExitcode(c.Process, runErr)
	case <-ctx.Done():
		return nonShellErrorCode, fmt.Errorf("remote end exited")
	}
}

// harvestExitcode returns the (exitcode, error) pair to be returned for the Shell RPC
// based on the status of the process and the error returned from runIOManager
func harvestExitcode(process *os.Process, ioerr error) (int32, error) {
	// Check the exit status.
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(process.Pid, &status, syscall.WNOHANG, nil); err != nil {
		return nonShellErrorCode, err
	}
	if status.Signaled() {
		return int32(status), fmt.Errorf("process killed by signal %u (%v)", status.Signal(), status.Signal())
	}
	if status.Exited() {
		if status.ExitStatus() == 0 {
			return 0, nil
		}
		return int32(status.ExitStatus()), fmt.Errorf("process exited with exit status %d", status.ExitStatus())
	}
	// The process has not exited. Use the error from ForwardStdIO.
	return nonShellErrorCode, ioerr
}

// findShell returns the path to the first usable shell binary.
func findShell() (string, error) {
	shells := []string{"/bin/bash", "/bin/sh"}
	for _, s := range shells {
		if _, err := os.Stat(s); err == nil {
			return s, nil
		}
	}
	return "", errors.New("could not find any shell binary")
}

// sendMotd sends the content of the MOTD file to the stream, if it exists.
func sendMotd(s tunnel.TunnelServiceShellStream) {
	data, err := ioutil.ReadFile("/etc/motd")
	if err != nil {
		// No MOTD. That's OK.
		return
	}
	packet := tunnel.ServerShellPacket{Stdout: []byte(data)}
	if err = s.SendStream().Send(packet); err != nil {
		vlog.Infof("Send failed: %v", err)
	}
}

func setWindowSize(fd uintptr, row, col uint32) {
	ws := lib.Winsize{Row: uint16(row), Col: uint16(col)}
	if err := lib.SetWindowSize(fd, ws); err != nil {
		vlog.Infof("Failed to set window size: %v", err)
	}
}
