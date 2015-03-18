package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"v.io/x/lib/vlog"
	"v.io/x/ref/examples/tunnel"
	"v.io/x/ref/examples/tunnel/tunnelutil"
)

func runIOManager(stdin io.Reader, stdout, stderr io.Writer, stream tunnel.TunnelShellClientCall) error {
	m := ioManager{stdin: stdin, stdout: stdout, stderr: stderr, stream: stream}
	return m.run()
}

// ioManager manages the forwarding of all the data between the shell and the
// stream.
type ioManager struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	stream         tunnel.TunnelShellClientCall

	// streamError receives errors coming from stream operations.
	streamError chan error
	// stdioError receives errors coming from stdio operations.
	stdioError chan error
}

func (m *ioManager) run() error {
	m.streamError = make(chan error, 1)
	m.stdioError = make(chan error, 1)

	var pendingUserInput sync.WaitGroup
	pendingUserInput.Add(1)
	var pendingStreamOutput sync.WaitGroup
	pendingStreamOutput.Add(1)

	// Forward data between the user and the remote shell.
	go func() {
		defer pendingUserInput.Done()
		// outchan is used to serialize the output to the stream.
		// chan2stream() receives data sent by handleWindowResize() and
		// user2outchan() and sends it to the stream.
		outchan := make(chan tunnel.ClientShellPacket)
		var wgStream sync.WaitGroup
		wgStream.Add(1)
		go m.chan2stream(outchan, &wgStream)

		// When the terminal window is resized, we receive a SIGWINCH. Then we
		// send the new window size to the server.
		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)

		var wgUser sync.WaitGroup
		wgUser.Add(2)
		go func() {
			m.user2outchan(outchan, &wgUser)
			signal.Stop(winch)
			close(winch)
		}()
		go m.handleWindowResize(winch, outchan, &wgUser)
		// When both user2outchan and handleWindowResize are done,
		// close outchan to signal chan2stream to exit.
		wgUser.Wait()
		close(outchan)
		wgStream.Wait()
	}()
	go m.stream2user(&pendingStreamOutput)
	// Block until something reports an error.
	select {
	case err := <-m.streamError:
		// When we receive an error from the stream, wait for any
		// remaining stream output to be sent to the user before
		// exiting.
		vlog.VI(2).Infof("run stream error: %v", err)
		pendingStreamOutput.Wait()
		return err
	case err := <-m.stdioError:
		// When we receive an error from the user, wait for any
		// remaining input from the user to be sent to the stream
		// before exiting.
		vlog.VI(2).Infof("run stdio error: %v", err)
		pendingUserInput.Wait()
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

// chan2stream receives ClientShellPacket from outchan and sends it to stream.
func (m *ioManager) chan2stream(outchan <-chan tunnel.ClientShellPacket, wg *sync.WaitGroup) {
	defer wg.Done()
	sender := m.stream.SendStream()
	for packet := range outchan {
		vlog.VI(3).Infof("chan2stream packet: %+v", packet)
		if err := sender.Send(packet); err != nil {
			vlog.VI(2).Infof("chan2stream: %v", err)
			m.sendStreamError(err)
		}
	}
	m.sendStreamError(io.EOF)
}

func (m *ioManager) handleWindowResize(winch <-chan os.Signal, outchan chan<- tunnel.ClientShellPacket, wg *sync.WaitGroup) {
	defer wg.Done()
	for _ = range winch {
		ws, err := tunnelutil.GetWindowSize()
		if err != nil {
			vlog.Infof("GetWindowSize failed: %v", err)
			continue
		}
		outchan <- tunnel.ClientShellPacketWinSize{tunnel.WindowSize{ws.Row, ws.Col}}
	}
}

// user2stream reads input from stdin and sends it to the outchan.
func (m *ioManager) user2outchan(outchan chan<- tunnel.ClientShellPacket, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		buf := make([]byte, 2048)
		n, err := m.stdin.Read(buf[:])
		if err == io.EOF {
			vlog.VI(2).Infof("user2outchan: EOF, closing stdin")
			outchan <- tunnel.ClientShellPacketEndOfFile{}
			return
		}
		if err != nil {
			vlog.VI(2).Infof("user2outchan: %v", err)
			m.sendStdioError(err)
			return
		}
		outchan <- tunnel.ClientShellPacketStdin{buf[:n]}
	}
}

// stream2user reads data from the stream and sends it to either stdout or stderr.
func (m *ioManager) stream2user(wg *sync.WaitGroup) {
	defer wg.Done()
	rStream := m.stream.RecvStream()
	for rStream.Advance() {
		packet := rStream.Value()
		vlog.VI(3).Infof("stream2user packet: %+v", packet)

		switch v := packet.(type) {
		case tunnel.ServerShellPacketStdout:
			if n, err := m.stdout.Write(v.Value); n != len(v.Value) || err != nil {
				m.sendStdioError(fmt.Errorf("stdout.Write returned (%d, %v) want (%d, nil)", n, err, len(v.Value)))
				return
			}
		case tunnel.ServerShellPacketStderr:
			if n, err := m.stderr.Write(v.Value); n != len(v.Value) || err != nil {
				m.sendStdioError(fmt.Errorf("stderr.Write returned (%d, %v) want (%d, nil)", n, err, len(v.Value)))
				return
			}
		default:
			vlog.Infof("unexpected message type: %T", packet)
		}
	}
	err := rStream.Err()
	if err == nil {
		err = io.EOF
	}
	vlog.VI(2).Infof("stream2user: %v", err)
	m.sendStreamError(err)
}
