package lib

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"veyron.io/veyron/veyron2/vlog"
)

// Used with ioctl TIOCGWINSZ and TIOCSWINSZ.
type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// SetWindowSize sets the terminal's window size.
func SetWindowSize(fd uintptr, ws Winsize) error {
	vlog.Infof("Setting window size: %v", ws)
	ret, _, _ := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&ws)))
	if int(ret) == -1 {
		return errors.New("ioctl(TIOCSWINSZ) failed")
	}
	return nil
}

// GetWindowSize gets the terminal's window size.
func GetWindowSize() (*Winsize, error) {
	ws := &Winsize{}
	ret, _, _ := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)))
	if int(ret) == -1 {
		return nil, errors.New("ioctl(TIOCGWINSZ) failed")
	}
	return ws, nil
}

func EnterRawTerminalMode() string {
	var savedBytes []byte
	var err error
	if savedBytes, err = exec.Command("stty", "-F", "/dev/tty", "-g").Output(); err != nil {
		vlog.Infof("Failed to save terminal settings: %q (%v)", savedBytes, err)
	}
	saved := strings.TrimSpace(string(savedBytes))

	args := []string{
		"-F", "/dev/tty",
		// Don't buffer stdin. Read characters as they are typed.
		"-icanon", "min", "1", "time", "0",
		// Turn off local echo of input characters.
		"-echo", "-echoe", "-echok", "-echonl",
		// Disable interrupt, quit, and suspend special characters.
		"-isig",
		// Ignore characters with parity errors.
		"ignpar",
		// Disable translate newline to carriage return.
		"-inlcr",
		// Disable ignore carriage return.
		"-igncr",
		// Disable translate carriage return to newline.
		"-icrnl",
		// Disable flow control.
		"-ixon", "-ixany", "-ixoff",
		// Disable non-POSIX special characters.
		"-iexten",
	}
	if out, err := exec.Command("stty", args...).CombinedOutput(); err != nil {
		vlog.Infof("stty failed (%v) (%q)", err, out)
	}

	return string(saved)
}

func RestoreTerminalSettings(saved string) {
	args := []string{
		"-F", "/dev/tty",
		saved,
	}
	if out, err := exec.Command("stty", args...).CombinedOutput(); err != nil {
		vlog.Infof("stty failed (%v) (%q)", err, out)
	}
}
