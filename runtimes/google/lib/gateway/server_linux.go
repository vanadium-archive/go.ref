package gateway

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"text/template"
	"time"

	"veyron2/rt"
	"veyron2/services/proximity"
	"veyron2/vlog"

	dbus "github.com/guelfey/go.dbus"
)

const (
	ipFwdFile  = "/proc/sys/net/ipv4/ip_forward"
	pingTarget = "www.google.com"
	bridgeName = "napbr0"
	// TODO(spetrovic): Instead of statically assigned addresses, scan
	// over all existing interfaces and find the one that's surely unused.
	bridgeIPAddr        = "192.168.9.1"
	bridgeIPSubnetShort = "192.168.9.0"
	bridgeIPSubnetLong  = "192.168.9.0/24"
	bridgeIPMask        = "255.255.255.0"
	bridgeIPBroadcast   = "192.168.9.255"
	bridgeDHCPStart     = "192.168.9.2"
	bridgeDHCPEnd       = "192.168.9.254"
	dhcpdTemplateStr    = `
# dhcpd config for the NAP bridge interface {{.Bridge}}
#
# {{.Bridge}} ip addr is expected to be {{.IPAddr}}
# serves {{.IPAddrStart}} - {{.IPAddrEnd}}

authoritative;

subnet {{.Subnet}} netmask {{.Netmask}} {
  interface "{{.Bridge}}";
  range {{.IPAddrStart}} {{.IPAddrEnd}};
  option routers {{.IPAddr}};
  option broadcast-address {{.Broadcast}};
}`
	bridgeUpTemplateStr = `
#!/bin/bash
# Script that creates and configures the bridge interface.

brctl addbr {{.Bridge}}
ifconfig {{.Bridge}} inet {{.BridgeAddr}} netmask {{.BridgeMask}} up
`
	bridgeDownTemplateStr = `
#!/bin/bash
# Script that creates and configures the bridge interface.

ifconfig {{.Bridge}} down
brctl delbr {{.Bridge}}
`
	natStartTemplateStr = `
#!/bin/bash
# Script that turns this server's NAT role on.
cp -f {{.IPFwdFile}} {{.IPFwdFile}}.old
echo "1" > {{.IPFwdFile}}
iptables -t nat -A POSTROUTING -s {{.BridgeSubnet}} -j MASQUERADE
iptables -A FORWARD -i {{.Bridge}} -o {{.Gateway}} -j ACCEPT
iptables -A FORWARD -o {{.Bridge}} -i {{.Gateway}} -j ACCEPT
`
	natStopTemplateStr = `
#!/bin/bash
# Script that turns this server's NAT role off.
cp -f {{.IPFwdFile}}.old {{.IPFwdFile}}
iptables -t nat -D POSTROUTING -s {{.BridgeSubnet}} -j MASQUERADE
iptables -D FORWARD -i {{.Bridge}} -o {{.Gateway}} -j ACCEPT
iptables -D FORWARD -o {{.Bridge}} -i {{.Gateway}} -j ACCEPT
`
)

var (
	dhcpdConfig        string
	dhcpdTemplate      = template.Must(template.New("dhcpd").Parse(dhcpdTemplateStr))
	bridgeUpTemplate   = template.Must(template.New("bridgeUp").Parse(bridgeUpTemplateStr))
	bridgeDownTemplate = template.Must(template.New("bridgeDown").Parse(bridgeDownTemplateStr))
	natStartTemplate   = template.Must(template.New("natStart").Parse(natStartTemplateStr))
	natStopTemplate    = template.Must(template.New("natStop").Parse(natStopTemplateStr))
)

func init() {
	d := struct {
		Bridge, IPAddr, IPAddrStart, IPAddrEnd, Subnet, Netmask, Broadcast string
	}{
		bridgeName, bridgeIPAddr, bridgeDHCPStart, bridgeDHCPEnd, bridgeIPSubnetShort, bridgeIPMask, bridgeIPBroadcast,
	}
	var err error
	if dhcpdConfig, err = fileFromTemplate(dhcpdTemplate, d, 0644); err != nil {
		panic(err)
	}
}

func newServer(p proximity.Proximity, gateway, device string) (*server, error) {
	s := &server{
		proximity: p,
		gateway:   gateway,
		device:    device,
		done:      make(chan bool),
	}
	err := s.configureBluetooth()
	if err == nil {
		err = s.bridgeUp()
	}
	if err == nil {
		err = s.startNAT()
	}
	if err == nil {
		err = s.startDHCP()
	}
	if err == nil {
		err = s.startNAP()
	}
	if err != nil {
		s.Stop()
		return nil, err
	}
	go s.rttLoop()
	return s, nil
}

type server struct {
	proximity       proximity.Proximity
	gateway, device string
	dhcpCmd         *exec.Cmd
	oldFwdVal       []byte
	conn            *dbus.Conn
	done            chan bool
	advertisedName  string
}

func (s *server) Stop() {
	s.stopNAP()
	s.stopDHCP()
	s.stopNAT()
	s.bridgeDown()
	close(s.done) // terminates rttLoop
}

func (s *server) bridgeUp() error {
	d := struct {
		Bridge, BridgeAddr, BridgeMask string
	}{
		bridgeName, bridgeIPAddr, bridgeIPMask,
	}
	if err := runScript(bridgeUpTemplate, d); err != nil {
		return fmt.Errorf("couldn't create bridge: %v", err)
	}
	return nil
}

func (s *server) bridgeDown() {
	d := struct {
		Bridge string
	}{
		bridgeName,
	}
	runScript(bridgeDownTemplate, d)
}

func (s *server) startNAT() error {
	d := struct {
		IPFwdFile, Bridge, Gateway, BridgeSubnet string
	}{
		ipFwdFile, bridgeName, s.gateway, bridgeIPSubnetLong,
	}
	if err := runScript(natStartTemplate, d); err != nil {
		return fmt.Errorf("couldn't start NAT: %v", err)
	}
	return nil
}

func (s *server) stopNAT() {
	d := struct {
		IPFwdFile, Bridge, Gateway, BridgeSubnet string
	}{
		ipFwdFile, bridgeName, s.gateway, bridgeIPSubnetLong,
	}
	runScript(natStopTemplate, d)
}

func (s *server) startDHCP() error {
	s.dhcpCmd = exec.Command("dhcpd", "-cf", dhcpdConfig, "-f", bridgeName)
	if err := s.dhcpCmd.Start(); err != nil {
		return fmt.Errorf("couldn't start dhcpd on bridge %q: %v", bridgeName, err)
	}
	return nil
}

func (s *server) stopDHCP() {
	if s.dhcpCmd == nil || s.dhcpCmd.Process == nil {
		// dhcpd not started?
		return
	}
	s.dhcpCmd.Process.Kill()
}

func (s *server) configureBluetooth() error {
	if err := exec.Command("hciconfig", s.device, "piscan").Run(); err != nil {
		return fmt.Errorf("couldn't make Bluetooth device %q discoverable: %v", s.device, err)
	}
	return nil
}

func (s *server) startNAP() error {
	var err error
	if s.conn, err = dbus.SystemBus(); err != nil {
		return fmt.Errorf("couldn't access system dbus: %v", err)
	}
	// Get the adapter.
	obj := s.conn.Object("org.bluez", dbus.ObjectPath("/"))
	var adapterPath dbus.ObjectPath
	if err := obj.Call("org.bluez.Manager.DefaultAdapter", 0).Store(&adapterPath); err != nil {
		return fmt.Errorf("couldn't find device %q: %v", s.device, err)
	}
	adapter := s.conn.Object("org.bluez", adapterPath)
	// Add a bluetooth agent.
	agent := &yesAgent{}
	if err = s.conn.Export(agent, agentPath, agentIface); err != nil {
		return fmt.Errorf("error exporting agent to dbus: %v", err)
	}
	if call := adapter.Call("org.bluez.Adapter.RegisterAgent", 0, agentPath, "NoInputNoOutput"); call.Err != nil {
		return fmt.Errorf("couldn't register agent: %v", call.Err)
	}
	// Register the NAP server.
	if call := adapter.Call("org.bluez.NetworkServer.Register", 0, "nap", bridgeName); call.Err != nil {
		return fmt.Errorf("couldn't start NAP server: %v", call.Err)
	}
	return nil
}

func (s *server) stopNAP() {
	if s.conn == nil {
		return
	}
	// Unregister the NAP server.
	obj := s.conn.Object("org.bluez", dbus.ObjectPath(fmt.Sprintf("/org/bluez/%s", s.device)))
	obj.Call("org.bluez.NetworkServer.Unegister", 0, "nap")

	// Remove the bluetooth agent.
	obj.Call("org.bluez.Adapter.UnregisterAgent", 0, agentPath)
	s.conn.Export(nil, agentPath, agentIface)
}

func (s *server) rttLoop() {
	defer vlog.VI(1).Info("gateway server's RTT loop exiting")
	for {
		select {
		case <-s.done:
			return
		case <-time.After(1 * time.Second):
		}

		rtt, err := rtt()
		if err != nil {
			vlog.Errorf("error getting RTT: %v", err)
			s.advertiseName("")
		} else {
			s.advertiseName(nameFromRTT(rtt))
		}
	}
}

func (s *server) advertiseName(name string) {
	if s.advertisedName != "" {
		if err := s.proximity.UnregisterName(rt.R().TODOContext(), s.advertisedName); err != nil {
			vlog.Errorf("error unregistering name %q with proximity service: %v", s.advertisedName, err)
		}
	}
	s.advertisedName = ""
	if name != "" {
		if err := s.proximity.RegisterName(rt.R().TODOContext(), name); err == nil {
			s.advertisedName = name
		} else {
			vlog.Errorf("error registering name %q with proximity service: %v", name, err)
		}
	}
}

func runScript(t *template.Template, data interface{}) error {
	fname, err := fileFromTemplate(t, data, 0777)
	if err != nil {
		return fmt.Errorf("couldn't create template file: %v", err)
	}
	defer os.Remove(fname)
	if err := exec.Command("bash", fname).Run(); err != nil {
		return fmt.Errorf("couldn't execute script %q: %v", fname, err)
	}
	return nil
}

func fileFromTemplate(t *template.Template, data interface{}, perm os.FileMode) (string, error) {
	// Create temp file and set the right permissions on it.
	f, err := ioutil.TempFile("", "tmp")
	if err != nil {
		return "", fmt.Errorf("couldn't create temp file: %v", err)
	}
	if err := os.Chmod(f.Name(), perm); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("couldn't change permissions on file %q to %v: err", f.Name(), perm, err)
	}

	// Execute the template and write it out into the temp file.
	if err := t.Execute(f, data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func rtt() (time.Duration, error) {
	v, err := ping(pingTarget)
	if err != nil {
		return 0, fmt.Errorf("error executing RTT command: %v", err)
	}
	if len(v) == 0 {
		return 0, errors.New("got empty RTT value")
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing RTT value %q: %v", v, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("negative RTT value: %f", f)
	}
	return time.Duration(int(f+0.5)) * time.Millisecond, nil
}
