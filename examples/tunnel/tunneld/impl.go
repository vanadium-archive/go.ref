// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

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

	"v.io/v23/context"
	"v.io/v23/logging"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/ref/examples/tunnel"
	"v.io/x/ref/examples/tunnel/internal"
)

// T implements tunnel.TunnelServerMethods
type T struct {
}

const nonShellErrorCode = 255

func (t *T) Forward(ctx *context.T, call tunnel.TunnelForwardServerCall, network, address string) error {
	conn, err := net.Dial(network, address)
	if err != nil {
		return err
	}
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	name := fmt.Sprintf("RemoteBlessings:%v LocalAddr:%v RemoteAddr:%v", b, conn.LocalAddr(), conn.RemoteAddr())
	ctx.Infof("TUNNEL START: %v", name)
	err = internal.Forward(conn, call.SendStream(), call.RecvStream())
	ctx.Infof("TUNNEL END  : %v (%v)", name, err)
	return err
}

func (t *T) ReverseForward(ctx *context.T, call rpc.ServerCall, network, address string) error {
	ln, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	defer ln.Close()
	ctx.Infof("Listening on %q", ln.Addr())
	remoteEP := call.RemoteEndpoint().Name()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				ctx.Infof("Accept failed: %v", err)
				if oErr, ok := err.(*net.OpError); ok && oErr.Temporary() {
					continue
				}
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				stream, err := tunnel.ForwarderClient(remoteEP).Forward(ctx)
				if err != nil {
					ctx.Infof("Forward failed: %v", err)
					return
				}
				name := fmt.Sprintf("%v-->%v-->(%v)", c.RemoteAddr(), c.LocalAddr(), remoteEP)
				ctx.Infof("TUNNEL START: %v", name)
				errf := internal.Forward(c, stream.SendStream(), stream.RecvStream())
				err = stream.Finish()
				ctx.Infof("TUNNEL END  : %v (%v, %v)", name, errf, err)
			}(conn)
		}
	}()
	<-ctx.Done()
	return nil
}

func (t *T) Shell(ctx *context.T, call tunnel.TunnelShellServerCall, command string, shellOpts tunnel.ShellOpts) (int32, error) {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("SHELL START for %v: %q", b, command)
	shell, err := findShell()
	if err != nil {
		return nonShellErrorCode, err
	}
	var c *exec.Cmd
	// An empty command means that we need an interactive shell.
	if len(command) == 0 {
		c = exec.Command(shell, "-i")
		sendMotd(ctx, call)
	} else {
		c = exec.Command(shell, "-c", command)
	}

	c.Env = []string{
		fmt.Sprintf("HOME=%s", os.Getenv("HOME")),
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
	}
	c.Env = append(c.Env, shellOpts.Environment...)
	ctx.Infof("Shell environment: %v", c.Env)

	c.Dir = os.Getenv("HOME")
	ctx.Infof("Shell CWD: %v", c.Dir)

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

		setWindowSize(ctx, ptyFd, shellOpts.WinSize.Rows, shellOpts.WinSize.Cols)
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
			ctx.Infof("Cmd.Start failed: %v", err)
			return nonShellErrorCode, err
		}
	}

	defer c.Process.Kill()

	select {
	case runErr := <-runIOManager(stdin, stdout, stderr, ptyFd, call):
		b, _ := security.RemoteBlessingNames(ctx, call.Security())
		ctx.Infof("SHELL END for %v: %q (%v)", b, command, runErr)
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
		return int32(status), fmt.Errorf("process killed by signal %d (%v)", status.Signal(), status.Signal())
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
	shells := []string{"/bin/bash", "/bin/sh", "/system/bin/sh"}
	for _, s := range shells {
		if _, err := os.Stat(s); err == nil {
			return s, nil
		}
	}
	return "", errors.New("could not find any shell binary")
}

// sendMotd sends the content of the MOTD file to the stream, if it exists.
func sendMotd(logger logging.Logger, s tunnel.TunnelShellServerStream) {
	data, err := ioutil.ReadFile("/etc/motd")
	if err != nil {
		// No MOTD. That's OK.
		return
	}
	packet := tunnel.ServerShellPacketStdout{[]byte(data)}
	if err = s.SendStream().Send(packet); err != nil {
		logger.Infof("Send failed: %v", err)
	}
}

func setWindowSize(logger logging.Logger, fd uintptr, row, col uint16) {
	ws := internal.Winsize{Row: row, Col: col}
	if err := internal.SetWindowSize(fd, ws); err != nil {
		logger.Infof("Failed to set window size: %v", err)
	}
}
