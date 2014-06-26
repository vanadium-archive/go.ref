package gateway

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"veyron2/vlog"

	dbus "github.com/guelfey/go.dbus"
)

const (
	agentPath  = dbus.ObjectPath("/org/veyron/internal/lib/gateway/yesagent")
	agentIface = "org.bluez.Agent"
)

var pingCmd = [][]string{
	{"ping", "-i", "0.1", "-c", "3", "-w", "1.0", "-q"},
	// TODO(spetrovic): Use Go's string libraries to reduce
	// dependence on these tools.
	{"grep", "rtt"},
	{"cut", "-d", " ", "-f", "4"},
	{"cut", "-d", "/", "-f", "2"},
	{"awk", `{printf "%s", $NF}`},
}

func runPipedCmd(cmd [][]string) (string, error) {
	if len(cmd) == 0 {
		return "", fmt.Errorf("invalid command %q", cmd)
	}
	for _, c := range cmd {
		if len(c) == 0 || len(c[0]) == 0 {
			return "", fmt.Errorf("invalid command %q", cmd)
		}
	}
	cs := make([]*exec.Cmd, len(cmd))
	for i, c := range cmd {
		cs[i] = exec.Command(c[0], c[1:]...)
	}
	for i := 1; i < len(cs); i++ {
		var err error
		if cs[i].Stdin, err = cs[i-1].StdoutPipe(); err != nil {
			return "", fmt.Errorf("piping error: %v", err)
		}
	}
	var out bytes.Buffer
	cs[len(cs)-1].Stdout = &out
	for _, c := range cs {
		if err := c.Start(); err != nil {
			return "", fmt.Errorf("error starting command %q: %q", c, err)
		}
	}
	for _, c := range cs {
		if err := c.Wait(); err != nil {
			return "", fmt.Errorf("error executing command %q: %q", c, err)
		}
	}
	return out.String(), nil
}

func ping(target string) (string, error) {
	cmd := make([][]string, len(pingCmd))
	copy(cmd, pingCmd)
	cmd[0] = make([]string, len(pingCmd[0]))
	copy(cmd[0], pingCmd[0])
	cmd[0] = append(cmd[0], target)
	return runPipedCmd(cmd)
}

func nameFromRTT(rtt time.Duration) string {
	return fmt.Sprintf("NAP(%v)", rtt)
}

func rttFromName(name string) (time.Duration, error) {
	if !strings.HasPrefix(name, "NAP(") || !strings.HasSuffix(name, ")") {
		return 0, fmt.Errorf("invalid string %q", name)
	}
	d, err := time.ParseDuration(name[4 : len(name)-1])
	if err != nil {
		return 0, fmt.Errorf("couldn't parse RTT in name %q: %v", name, err)
	}
	return d, nil
}

// yesAgent implements the org.bluez.Agent DBus interface, allowing two devices
// to connect without any security. This DBus interface is compatible
// with bluez-4.101 package.
type yesAgent struct{}

func (*yesAgent) Release() *dbus.Error {
	vlog.VI(1).Info("Release() called")
	return nil
}

func (*yesAgent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	vlog.VI(1).Info("RequestPinCode(%q) called", device)
	return "1234", nil
}

func (*yesAgent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	vlog.VI(1).Info("RequestPasskey(%q) called", device)
	return 123456, nil
}

func (*yesAgent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint8) *dbus.Error {
	vlog.VI(1).Info("DisplayPasskey(%q, %d, %d) called", device, passkey, entered)
	return nil
}

func (*yesAgent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	vlog.VI(1).Info("DisplayPinCode(%q, %q) called", device, pincode)
	return nil
}

func (*yesAgent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	vlog.VI(1).Info("RequestConfirmation(%q, %d) called", device, passkey)
	return nil
}

func (*yesAgent) Authorize(device dbus.ObjectPath, uuid string) *dbus.Error {
	vlog.VI(1).Info("AuthorizeService(%q, %q) called", device, uuid)
	return nil
}

func (*yesAgent) ConfirmModeChange(mode string) *dbus.Error {
	vlog.VI(1).Info("ConfirmModeChange(%q) called", mode)
	return nil
}

func (*yesAgent) Cancel() *dbus.Error {
	vlog.VI(1).Info("Cancel() called")
	return nil
}
