package exec

import (
	"errors"
	"os"
)

var (
	ErrNoVersion          = errors.New(versionVariable + " environment variable missing")
	ErrUnsupportedVersion = errors.New("Unsupported version of veyron/runtimes/google/lib/exec request by " + versionVariable + " environment variable")
)

type ChildHandle struct {
	// A Secret passed to the child by its parent via a trusted channel
	Secret     string
	statusPipe *os.File
}

// fileOffset accounts for the file descriptors that are always passed to the
// child by the parent: stderr, stdin, stdout, token read, and status write.
// Any extra files added by the client will follow fileOffset.
const fileOffset = 5

// NewChildHandle creates a new ChildHandle that can be used to signal
// that the child is 'ready' (by calling SetReady) to its parent. The
// value of the ChildHandle's Secret securely passed to it by the parent; this
// is intended for subsequent use to create a secure communication channels
// and or authentication.
//
// If the child is relying on exec.Cmd.ExtraFiles then its first file descriptor
// will not be 3, but will be offset by extra files added by the framework.  The
// developer should use the NewExtraFile method to robustly get their extra
// files with the correct offset applied.
func NewChildHandle() (*ChildHandle, error) {
	switch os.Getenv(versionVariable) {
	case "":
		return nil, ErrNoVersion
	case version1:
		// TODO(cnicolaou): need to use major.minor.build format for version #s
	default:
		return nil, ErrUnsupportedVersion
	}
	tokenPipe := os.NewFile(3, "token_rd")

	buf := make([]byte, MaxSecretSize)
	n, err := tokenPipe.Read(buf)
	if err != nil {
		return nil, err
	}
	c := &ChildHandle{
		Secret:     string(buf[:n]),
		statusPipe: os.NewFile(4, "status_wr"),
	}
	return c, nil
}

// SetReady writes a 'ready' status to its parent.
func (c *ChildHandle) SetReady() error {
	_, err := c.statusPipe.Write([]byte(readyStatus))
	c.statusPipe.Close()
	return err
}

// NewExtraFile creates a new file handle for the i-th file descriptor after
// discounting stdout, stderr, stdin and the files reserved by the framework for
// its own purposes.
func (c *ChildHandle) NewExtraFile(i uintptr, name string) *os.File {
	return os.NewFile(i+fileOffset, name)
}
