package exec

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
)

var (
	ErrNoVersion          = errors.New(VersionVariable + " environment variable missing")
	ErrUnsupportedVersion = errors.New("Unsupported version of veyron/lib/exec request by " + VersionVariable + " environment variable")
)

type ChildHandle struct {
	// Config is passed down from the parent.
	Config Config
	// Secret is a secret passed to the child by its parent via a
	// trusted channel.
	Secret string
	// statusPipe is a pipe that is used to notify the parent that the child
	// process has started successfully. It is meant to be invoked by the
	// veyron framework to notify the parent that the child process has
	// successfully started.
	statusPipe *os.File
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

// SetReady writes a 'ready' status to its parent.
func (c *ChildHandle) SetReady() error {
	_, err := c.statusPipe.Write([]byte(readyStatus))
	c.statusPipe.Close()
	return err
}

// SetFailed writes a 'failed' status to its parent.
func (c *ChildHandle) SetFailed(oerr error) error {
	_, err := c.statusPipe.Write([]byte(failedStatus + oerr.Error()))
	c.statusPipe.Close()
	return err
}

// NewExtraFile creates a new file handle for the i-th file descriptor after
// discounting stdout, stderr, stdin and the files reserved by the framework for
// its own purposes.
func (c *ChildHandle) NewExtraFile(i uintptr, name string) *os.File {
	return os.NewFile(i+FileOffset, name)
}

func createChildHandle() (*ChildHandle, error) {
	switch os.Getenv(VersionVariable) {
	case "":
		return nil, ErrNoVersion
	case version1:
		os.Setenv(VersionVariable, "")
		// TODO(cnicolaou): need to use major.minor.build format for
		// version #s.
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
