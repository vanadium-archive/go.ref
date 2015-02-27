package main

import (
	"fmt"
	"io"
	"sync"

	"v.io/apps/tunnel"
	"v.io/x/lib/vlog"
)

func runIOManager(stdin io.WriteCloser, stdout, stderr io.Reader, ptyFd uintptr, stream tunnel.TunnelShellServerStream) <-chan error {
	m := ioManager{stdin: stdin, stdout: stdout, stderr: stderr, ptyFd: ptyFd, stream: stream}
	c := make(chan error, 1) // buffered channel so that the goroutine spawned below is not leaked if the channel is not read from.
	go func() { c <- m.run() }()
	return c
}

// ioManager manages the forwarding of all the data between the shell and the
// stream.
type ioManager struct {
	stdin          io.WriteCloser
	stdout, stderr io.Reader
	ptyFd          uintptr
	stream         tunnel.TunnelShellServerStream

	// streamError receives errors coming from stream operations.
	streamError chan error
	// stdioError receives errors coming from stdio operations.
	stdioError chan error
}

func (m *ioManager) run() error {
	m.streamError = make(chan error, 1)
	m.stdioError = make(chan error, 1)

	var pendingShellOutput sync.WaitGroup
	pendingShellOutput.Add(1)
	var pendingStreamInput sync.WaitGroup
	pendingStreamInput.Add(1)

	// Forward data between the shell's stdio and the stream.
	go func() {
		defer pendingShellOutput.Done()
		// outchan is used to serialize the output to the stream.
		// chan2stream() receives data sent by stdout2outchan() and
		// stderr2outchan() and sends it to the stream.
		outchan := make(chan tunnel.ServerShellPacket)
		var wgStream sync.WaitGroup
		wgStream.Add(1)
		go m.chan2stream(outchan, &wgStream)
		var wgStdio sync.WaitGroup
		wgStdio.Add(1)
		go m.stdout2outchan(outchan, &wgStdio)
		if m.stderr != nil {
			wgStdio.Add(1)
			go m.stderr2outchan(outchan, &wgStdio)
		}
		// When both stdout2outchan and stderr2outchan are done, close
		// outchan to signal chan2stream to exit.
		wgStdio.Wait()
		close(outchan)
		wgStream.Wait()
	}()
	go m.stream2stdin(&pendingStreamInput)

	// Block until something reports an error.
	//
	// If there is any stream error, we assume that both ends of the stream
	// have an error, e.g. if stream.Reader.Advance fails then
	// stream.Sender.Send will fail. We process any remaining input from
	// the stream and then return.
	//
	// If there is any stdio error, we assume all 3 io channels will fail
	// (if stdout.Read fails then stdin.Write and stderr.Read will also
	// fail). We process is remaining output from the shell and then
	// return.
	select {
	case err := <-m.streamError:
		// Process remaining input from the stream before exiting.
		vlog.VI(2).Infof("run stream error: %v", err)
		pendingStreamInput.Wait()
		return err
	case err := <-m.stdioError:
		// Process remaining output from the shell before exiting.
		vlog.VI(2).Infof("run stdio error: %v", err)
		pendingShellOutput.Wait()
		return err
	}
}

func (m *ioManager) sendStreamError(err error) {
	select {
	case m.streamError <- err:
	default:
	}
}

func (m *ioManager) sendStdioError(err error) {
	select {
	case m.stdioError <- err:
	default:
	}
}

// chan2stream receives ServerShellPacket from outchan and sends it to stream.
func (m *ioManager) chan2stream(outchan <-chan tunnel.ServerShellPacket, wg *sync.WaitGroup) {
	defer wg.Done()
	sender := m.stream.SendStream()
	for packet := range outchan {
		vlog.VI(3).Infof("chan2stream packet: %+v", packet)
		if err := sender.Send(packet); err != nil {
			vlog.VI(2).Infof("chan2stream: %v", err)
			m.sendStreamError(err)
		}
	}
}

// stdout2stream reads data from the shell's stdout and sends it to the outchan.
func (m *ioManager) stdout2outchan(outchan chan<- tunnel.ServerShellPacket, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		buf := make([]byte, 2048)
		n, err := m.stdout.Read(buf[:])
		if err != nil {
			vlog.VI(2).Infof("stdout2outchan: %v", err)
			m.sendStdioError(err)
			return
		}
		outchan <- tunnel.ServerShellPacketStdout{buf[:n]}
	}
}

// stderr2stream reads data from the shell's stderr and sends it to the outchan.
func (m *ioManager) stderr2outchan(outchan chan<- tunnel.ServerShellPacket, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		buf := make([]byte, 2048)
		n, err := m.stderr.Read(buf[:])
		if err != nil {
			vlog.VI(2).Infof("stderr2outchan: %v", err)
			m.sendStdioError(err)
			return
		}
		outchan <- tunnel.ServerShellPacketStderr{buf[:n]}
	}
}

// stream2stdin reads data from the stream and sends it to the shell's stdin.
func (m *ioManager) stream2stdin(wg *sync.WaitGroup) {
	defer wg.Done()
	rStream := m.stream.RecvStream()
	for rStream.Advance() {
		packet := rStream.Value()
		vlog.VI(3).Infof("stream2stdin packet: %+v", packet)
		switch v := packet.(type) {
		case tunnel.ClientShellPacketStdin:
			if n, err := m.stdin.Write(v.Value); n != len(v.Value) || err != nil {
				m.sendStdioError(fmt.Errorf("stdin.Write returned (%d, %v) want (%d, nil)", n, err, len(v.Value)))
				return
			}
		case tunnel.ClientShellPacketEOF:
			if err := m.stdin.Close(); err != nil {
				m.sendStdioError(fmt.Errorf("stdin.Close: %v", err))
				return
			}
		case tunnel.ClientShellPacketWinSize:
			size := v.Value
			if size.Rows > 0 && size.Cols > 0 && m.ptyFd != 0 {
				setWindowSize(m.ptyFd, size.Rows, size.Cols)
			}
		default:
			vlog.Infof("unexpected message type: %T", packet)
		}
	}

	err := rStream.Err()
	if err == nil {
		err = io.EOF
	}

	vlog.VI(2).Infof("stream2stdin: %v", err)
	m.sendStreamError(err)
	if err := m.stdin.Close(); err != nil {
		m.sendStdioError(fmt.Errorf("stdin.Close: %v", err))
	}
}
