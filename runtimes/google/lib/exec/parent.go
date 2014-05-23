package exec

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"veyron/runtimes/google/lib/timekeeper"

	"veyron2/vlog"
)

var (
	ErrAuthTimeout    = errors.New("timout in auth handshake")
	ErrTimeout        = errors.New("timeout waiting for child")
	ErrSecretTooLarge = errors.New("secret is too large")
)

// A ParentHandle is the Parent process' means of managing a single child.
type ParentHandle struct {
	c                    *exec.Cmd
	secret               string
	statusRead           *os.File
	statusWrite          *os.File
	tk                   timekeeper.TimeKeeper
	doneStatus, doneWait sync.WaitGroup
}

// ParentHandleOpt is an option for NewParentHandle.
type ParentHandleOpt interface {
	// ExecParentHandleOpt is a signature 'dummy' method for the interface.
	ExecParentHandleOpt()
}

// TimeKeeperOpt can be used to seed the parent handle with a custom timekeeper.
type TimeKeeperOpt struct {
	tk timekeeper.TimeKeeper
}

// ExecParentHandleOpt makes TimeKeeperOpt an instance of ParentHandleOpt.
func (tko TimeKeeperOpt) ExecParentHandleOpt() {}

// NewParentHandle creates a ParentHandle for the child process represented by
// an instance of exec.Cmd.
func NewParentHandle(c *exec.Cmd, secret string, opts ...ParentHandleOpt) *ParentHandle {
	c.Env = append(c.Env, versionVariable+"="+version1)
	if len(secret) == 0 {
		secret = emptySecret
	}
	var tk timekeeper.TimeKeeper
	for _, opt := range opts {
		switch v := opt.(type) {
		case TimeKeeperOpt:
			tk = v.tk
		default:
			vlog.Errorf("Unrecognized parent option: %v", v)
		}
	}
	if tk == nil {
		tk = timekeeper.RealTime()
	}
	return &ParentHandle{
		c:      c,
		secret: secret,
		tk:     tk,
	}
}

// Start starts the child process, sharing a secret with it and
// setting up a communication channel over which to read its status.
func (p *ParentHandle) Start() error {
	if len(p.secret) > MaxSecretSize {
		return ErrSecretTooLarge
	}
	tokenRead, tokenWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer tokenWrite.Close()
	defer tokenRead.Close()

	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	p.statusRead = statusRead
	p.statusWrite = statusWrite

	extraFiles := make([]*os.File, len(p.c.ExtraFiles)+2)
	extraFiles[0] = tokenRead
	extraFiles[1] = statusWrite
	for i, _ := range p.c.ExtraFiles {
		extraFiles[i+2] = p.c.ExtraFiles[i]
	}
	p.c.ExtraFiles = extraFiles
	if err := p.c.Start(); err != nil {
		p.statusWrite.Close()
		p.statusRead.Close()
		return err
	}

	if _, err = tokenWrite.Write([]byte(p.secret)); err != nil {
		p.statusWrite.Close()
		p.statusRead.Close()
		return err
	}
	return nil
}

func waitForStatus(c chan string, e chan error, r *os.File, done *sync.WaitGroup) {
	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		e <- err
	} else {
		c <- string(buf[:n])
	}
	r.Close()
	close(c)
	close(e)
	done.Done()
}

// WaitForReady will wait for the child process to become ready.
func (p *ParentHandle) WaitForReady(timeout time.Duration) error {
	defer p.statusWrite.Close()
	c := make(chan string, 1)
	e := make(chan error, 1)
	p.doneStatus.Add(1)
	go waitForStatus(c, e, p.statusRead, &p.doneStatus)
	for {
		select {
		case err := <-e:
			return err
		case st := <-c:
			if st == readyStatus {
				return nil
			}
		case <-p.tk.After(timeout):
			// Make sure that the read in waitForStatus
			// returns now.
			p.statusWrite.Write([]byte("quit"))
			return ErrTimeout
		}
	}
	panic("unreachable")
}

// Wait will wait for the child process to terminate of its own accord.
// It returns nil if the process exited cleanly with an exit status of 0,
// any other exit code or error will result in an appropriate error return
func (p *ParentHandle) Wait(timeout time.Duration) error {
	c := make(chan error, 1)
	p.doneWait.Add(1)
	go func() {
		c <- p.c.Wait()
		close(c)
		p.doneWait.Done()
	}()
	// If timeout is zero time.After will panic; we handle zero specially
	// to mean infinite timeout.
	if timeout > 0 {
		select {
		case <-p.tk.After(timeout):
			return ErrTimeout
		case err := <-c:
			return err
		}
	} else {
		return <-c
	}
	panic("unreachable")
}

// Kill kills the child process.
func (p *ParentHandle) Kill() error {
	return p.c.Process.Kill()
}

// Signal sends the given signal to the child process.
func (p *ParentHandle) Signal(sig syscall.Signal) error {
	return syscall.Kill(p.c.Process.Pid, sig)
}

// Clean will clean up state, including killing the child process.
func (p *ParentHandle) Clean() error {
	if err := p.Kill(); err != nil {
		return err
	}
	return p.c.Wait()
}
