package impl

import (
	"fmt"
	"io"

	"veyron/examples/tunnel"
	"veyron2/vlog"
)

func runIOManager(stdin io.Writer, stdout, stderr io.Reader, ptyFd uintptr, stream tunnel.TunnelServiceShellStream) error {
	m := ioManager{stdin: stdin, stdout: stdout, stderr: stderr, ptyFd: ptyFd, stream: stream}
	return m.run()
}

// ioManager manages the forwarding of all the data between the shell and the
// stream.
type ioManager struct {
	stdin          io.Writer
	stdout, stderr io.Reader
	ptyFd          uintptr
	stream         tunnel.TunnelServiceShellStream

	// done receives any error from chan2stream, user2stream, or
	// stream2user.
	done chan error
	// outchan is used to serialize the output to the stream. This is
	// needed because stream.Send is not thread-safe.
	outchan chan tunnel.ServerShellPacket
	// closed is closed when run() exits.
	closed chan struct{}
}

func (m *ioManager) run() error {
	// done receives any error from chan2stream, stdout2stream, or
	// stream2stdin.
	m.done = make(chan error, 3)
	// outchan is used to serialize the output to the stream.
	// chan2stream() receives data sent by stdout2outchan() and
	// stderr2outchan() and sends it to the stream.
	m.outchan = make(chan tunnel.ServerShellPacket)
	m.closed = make(chan struct{})
	defer close(m.closed)
	go m.chan2stream()

	// Forward data between the shell's stdio and the stream.
	go m.stdout2outchan()
	if m.stderr != nil {
		go m.stderr2outchan()
	}
	go m.stream2stdin()

	// Block until something reports an error.
	return <-m.done
}

// chan2stream receives ServerShellPacket from outchan and sends it to stream.
func (m *ioManager) chan2stream() {
	sender := m.stream.SendStream()
	for packet := range m.outchan {
		if err := sender.Send(packet); err != nil {
			m.done <- err
			return
		}
	}
	m.done <- io.EOF
}

func (m *ioManager) sendOnOutchan(p tunnel.ServerShellPacket) bool {
	select {
	case m.outchan <- p:
		return true
	case <-m.closed:
		return false
	}
}

// stdout2stream reads data from the shell's stdout and sends it to the outchan.
func (m *ioManager) stdout2outchan() {
	for {
		buf := make([]byte, 2048)
		n, err := m.stdout.Read(buf[:])
		if err != nil {
			vlog.VI(2).Infof("stdout2outchan: %v", err)
			m.done <- err
			return
		}
		if !m.sendOnOutchan(tunnel.ServerShellPacket{Stdout: buf[:n]}) {
			return
		}
	}
}

// stderr2stream reads data from the shell's stderr and sends it to the outchan.
func (m *ioManager) stderr2outchan() {
	for {
		buf := make([]byte, 2048)
		n, err := m.stderr.Read(buf[:])
		if err != nil {
			vlog.VI(2).Infof("stderr2outchan: %v", err)
			m.done <- err
			return
		}
		if !m.sendOnOutchan(tunnel.ServerShellPacket{Stderr: buf[:n]}) {
			return
		}
	}
}

// stream2stdin reads data from the stream and sends it to the shell's stdin.
func (m *ioManager) stream2stdin() {
	rStream := m.stream.RecvStream()
	for rStream.Advance() {
		packet := rStream.Value()
		if len(packet.Stdin) > 0 {
			if n, err := m.stdin.Write(packet.Stdin); n != len(packet.Stdin) || err != nil {
				m.done <- fmt.Errorf("stdin.Write returned (%d, %v) want (%d, nil)", n, err, len(packet.Stdin))
				return
			}
		}
		if packet.Rows > 0 && packet.Cols > 0 && m.ptyFd != 0 {
			setWindowSize(m.ptyFd, packet.Rows, packet.Cols)
		}
	}

	err := rStream.Err()
	if err == nil {
		err = io.EOF
	}

	vlog.VI(2).Infof("stream2stdin: %v", err)
	m.done <- err
}
