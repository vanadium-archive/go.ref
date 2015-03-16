package exec

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"unicode/utf8"

	"v.io/x/ref/lib/exec/consts"
)

var (
	ErrNoVersion          = errors.New(consts.ExecVersionVariable + " environment variable missing")
	ErrUnsupportedVersion = errors.New("Unsupported version of veyron/lib/exec request by " + consts.ExecVersionVariable + " environment variable")
)

type ChildHandle struct {
	// Config is passed down from the parent.
	Config Config
	// Secret is a secret passed to the child by its parent via a
	// trusted channel.
	Secret string
	// statusPipe is a pipe that is used to notify the parent that the child
	// process has started successfully. It is meant to be invoked by the
	// vanadium framework to notify the parent that the child process has
	// successfully started.
	statusPipe *os.File

	// statusMu protexts sentStatus and statusErr and prevents us from trying to
	// send multiple status updates to the parent.
	statusMu sync.Mutex
	// sentStatus records the status that was already sent to the parent.
	sentStatus string
	// statusErr records the error we received, if any, when sentStatus
	// was sent.
	statusErr error
}

var (
	childHandle    *ChildHandle
	childHandleErr error
	once           sync.Once
)

// FileOffset accounts for the file descriptors that are always passed
// to the child by the parent: stderr, stdin, stdout, data read, and
// status write. Any extra files added by the client will follow
// fileOffset.
const FileOffset = 5

// GetChildHandle returns a ChildHandle that can be used to signal
// that the child is 'ready' (by calling SetReady) to its parent or to
// retrieve data securely passed to this process by its parent. For
// instance, a secret intended to create a secure communication
// channels and or authentication.
//
// If the child is relying on exec.Cmd.ExtraFiles then its first file
// descriptor will not be 3, but will be offset by extra files added
// by the framework. The developer should use the NewExtraFile method
// to robustly get their extra files with the correct offset applied.
func GetChildHandle() (*ChildHandle, error) {
	once.Do(func() {
		childHandle, childHandleErr = createChildHandle()
	})
	return childHandle, childHandleErr
}

func (c *ChildHandle) writeStatus(status, detail string) error {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()

	if c.sentStatus == "" {
		c.sentStatus = status
		status = status + detail
		toWrite := make([]byte, 0, len(status))
		var buf [utf8.UTFMax]byte
		// This replaces any invalid utf-8 bytes in the status string with the
		// Unicode replacement character.  This ensures that we only send valid
		// utf-8 (followed by the eofChar).
		for _, r := range status {
			n := utf8.EncodeRune(buf[:], r)
			toWrite = append(toWrite, buf[:n]...)
		}
		toWrite = append(toWrite, eofChar)
		_, c.statusErr = c.statusPipe.Write(toWrite)
		c.statusPipe.Close()
	} else if c.sentStatus != status {
		return errors.New("A different status: " + c.sentStatus + " has already been sent.")
	}
	return c.statusErr
}

// SetReady writes a 'ready' status to its parent.
// Only one of SetReady or SetFailed can be called, attempting to send
// both will fail.  In addition the status is only sent once to the parent
// subsequent calls will return immediately with the same error that was
// returned on the first call (possibly nil).
func (c *ChildHandle) SetReady() error {
	return c.writeStatus(readyStatus, strconv.Itoa(os.Getpid()))
}

// SetFailed writes a 'failed' status to its parent.
func (c *ChildHandle) SetFailed(oerr error) error {
	return c.writeStatus(failedStatus, oerr.Error())
}

// NewExtraFile creates a new file handle for the i-th file descriptor after
// discounting stdout, stderr, stdin and the files reserved by the framework for
// its own purposes.
func (c *ChildHandle) NewExtraFile(i uintptr, name string) *os.File {
	return os.NewFile(i+FileOffset, name)
}

func createChildHandle() (*ChildHandle, error) {
	// TODO(cnicolaou): need to use major.minor.build format for
	// version #s.
	switch os.Getenv(consts.ExecVersionVariable) {
	case "":
		return nil, ErrNoVersion
	case version1:
		os.Setenv(consts.ExecVersionVariable, "")
	default:
		return nil, ErrUnsupportedVersion
	}
	dataPipe := os.NewFile(3, "data_rd")
	serializedConfig, err := decodeString(dataPipe)
	if err != nil {
		return nil, err
	}
	cfg := NewConfig()
	if err := cfg.MergeFrom(serializedConfig); err != nil {
		return nil, err
	}
	secret, err := decodeString(dataPipe)
	if err != nil {
		return nil, err
	}
	childHandle = &ChildHandle{
		Config:     cfg,
		Secret:     secret,
		statusPipe: os.NewFile(4, "status_wr"),
	}
	return childHandle, nil
}

func decodeString(r io.Reader) (string, error) {
	var l int64 = 0
	if err := binary.Read(r, binary.BigEndian, &l); err != nil {
		return "", err
	}
	var data []byte = make([]byte, l)
	if n, err := r.Read(data); err != nil || int64(n) != l {
		if err != nil {
			return "", err
		} else {
			return "", errors.New("partial read")
		}
	}
	return string(data), nil
}
