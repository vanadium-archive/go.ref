package gateway

import (
	"fmt"
	"math"
	"net"
	"os/exec"
	"strings"
	"time"

	"veyron/runtimes/google/lib/unit"
	"veyron2/rt"
	"veyron2/services/proximity"
	"veyron2/vlog"

	dbus "github.com/guelfey/go.dbus"
)

// Bluetooth service UUID string for the NAP service.
const napUUID = "0x1116"

type device struct {
	mac      net.HardwareAddr
	distance unit.Distance
	rtt      time.Duration
}

func (d *device) String() string {
	return d.mac.String()
}

// affinity returns a value in the range [0, inf), indicating how beneficial
// this device would be as a gateway.  When choosing gateways, smaller affinity
// values are preferred to larger ones.
func (d *device) affinity() float64 {
	if d.rtt.Seconds() < 0 || d.distance.Millimeters() < 0 {
		return math.Inf(1)
	}
	if d.distance.Millimeters() == 0 {
		return 0
	}
	return d.rtt.Seconds() / d.distance.Millimeters()
}

type client struct {
	proximity proximity.Proximity
	conn      *dbus.Conn
	done      chan bool
}

func newClient(p proximity.Proximity) (*client, error) {
	c := &client{
		proximity: p,
		done:      make(chan bool),
	}

	// Connect to DBus and export a bluetooth agent.
	var err error
	if c.conn, err = dbus.SystemBus(); err != nil {
		return nil, fmt.Errorf("couldn't access system dbus: %v", err)
	}
	if err = c.conn.Export(&yesAgent{}, agentPath, agentIface); err != nil {
		return nil, fmt.Errorf("error exporting agent to dbus: %v", err)
	}

	// Start background loops.
	go c.updateLoop()

	return c, nil
}

func (c *client) Stop() {
	c.conn.Export(nil, agentPath, agentIface)
	close(c.done)
}

// updateLoop obtains a list of nearby devices from proximity service
// and updates the local gateway, if needed.
func (c *client) updateLoop() {
	defer vlog.VI(1).Infof("device update loop exiting.")
	t := time.Tick(1 * time.Second)
	var gateway *net.HardwareAddr
	var gatewayIface string
	for {
		select {
		case <-c.done:
			return
		case <-t:
		}

		nearby, err := c.proximity.NearbyDevices(rt.R().TODOContext())
		if err != nil {
			vlog.Errorf("error getting nearby devices list: %v", err)
			continue
		}

		// Find the device with the smallest affinity.
		var d, g *device
		min := math.Inf(1)
		for _, n := range nearby {
			mac, err := net.ParseMAC(n.MAC)
			if err != nil {
				vlog.Errorf("error parsing MAC address %q: %v", n.MAC, err)
				continue
			}
			distance, err := unit.ParseDistance(n.Distance)
			if err != nil {
				vlog.Errorf("error parsing distance %q: %v", n.Distance, err)
				continue
			}
			rtt, found := rttFromNames(n.Names)
			if !found {
				continue
			}
			dev := &device{
				mac:      mac,
				distance: distance,
				rtt:      rtt,
			}
			if dev.affinity() < min {
				d = dev
				min = dev.affinity()
			}
			if gateway != nil && gateway.String() == dev.mac.String() {
				g = dev
			}
		}

		if shouldUpdateGateway(g, d) {
			vlog.VI(1).Infof("connecting new gateway %q", d.mac)
			if iface, err := c.connectDevice(d.mac); err == nil {
				vlog.VI(1).Infof("connected")
				if gateway != nil {
					vlog.VI(1).Infof("disconnecting (old) gateway %q", *gateway)
					c.disconnectDevice(*gateway)
					vlog.VI(1).Infof("disconnected")
				}
				gateway = &d.mac
				gatewayIface = iface
			} else {
				vlog.Errorf("couldn't connect to new gateway %q: %v", d, err)
			}
		}

		// Test if the gateway is (still) functional and if not
		// disconnect it.
		if len(gatewayIface) > 0 && !testInterface(gatewayIface) {
			// Gateway not functional - disconnect it.
			vlog.VI(1).Infof("disconnecting non-functional gateway %q", *gateway)
			c.disconnectDevice(*gateway)
			gateway = nil
			gatewayIface = ""
		}
	}
}

func (c *client) connectDevice(mac net.HardwareAddr) (string, error) {
	// Find the local bluetooth adapter.
	obj := c.conn.Object("org.bluez", dbus.ObjectPath("/"))
	var adapterPath dbus.ObjectPath
	if err := obj.Call("org.bluez.Manager.DefaultAdapter", 0).Store(&adapterPath); err != nil {
		return "", fmt.Errorf("couldn't find bluetooth adapter: %v", err)
	}
	adapter := c.conn.Object("org.bluez", adapterPath)

	// Create a new local device object corresponding to the remote device.
	var devicePath dbus.ObjectPath
	if err := adapter.Call("org.bluez.Adapter.FindDevice", 0, mac.String()).Store(&devicePath); err != nil {
		// Local device object doesn't exist - create it.
		if err := adapter.Call("org.bluez.Adapter.CreateDevice", 0, mac.String()).Store(&devicePath); err != nil {
			return "", fmt.Errorf("couldn't create local device object %q: %v", mac, err)
		}
	}
	// Re-discover the Network service for the remote device.  This will
	// ensure that we have local bindings for the NAP-connect call below.
	dev := c.conn.Object("org.bluez", devicePath)
	if call := dev.Call("org.bluez.Device.DiscoverServices", 0, napUUID); call.Err != nil {
		vlog.Errorf("couldn't (re)discover Network service for device %q: %v", mac, call.Err)
	}

	// Pair with the device.  We ignore any errors because an already-paired
	// device returns it as well.  (Should the pairing legitimately fail,
	// the NAP-connect method below will fail.)
	adapter.Call("org.bluez.Adapter.CreatePairedDevice", 0, mac.String(), agentPath, "NoInputNoOutput")

	// Disconnect the device for good measure, in case we are connected but
	// don't know it. (This can happen if, for example, the server
	// terminated the connection and it didn't get cleaned up correctly.)
	c.disconnectDevice(mac)

	// Establish (a new) NAP connection to the device.
	var iface string
	if err := dev.Call("org.bluez.Network.Connect", 0).Store(&iface); err != nil {
		return "", fmt.Errorf("couldn't establish NAP connection to device %q: %v", mac, err)
	}

	// Start DHCP client.
	cmd := exec.Command("dhcpcd", "-4", "-C", "resolv.conf", "-C", "mtu", iface)
	if err := cmd.Run(); err != nil {
		c.disconnectDevice(mac)
		return "", fmt.Errorf("couldn't start dhcpcd for device %q: %v", mac, err)
	}

	return iface, nil
}

func (c *client) disconnectDevice(mac net.HardwareAddr) {
	obj := c.conn.Object("org.bluez", dbus.ObjectPath("/"))
	var adapterPath dbus.ObjectPath
	if err := obj.Call("org.bluez.Manager.DefaultAdapter", 0).Store(&adapterPath); err != nil {
		return
	}
	adapter := c.conn.Object("org.bluez", adapterPath)

	var devicePath dbus.ObjectPath
	if err := adapter.Call("org.bluez.Adapter.FindDevice", 0, mac.String()).Store(&devicePath); err != nil {
		return
	}

	device := c.conn.Object("org.bluez", devicePath)
	device.Call("org.bluez.Network.Disconnect", 0)
}

func shouldUpdateGateway(curg, newg *device) bool {
	if newg == nil {
		return false
	}
	if curg == nil {
		return true
	}
	return curg.mac.String() != newg.mac.String() && curg.affinity() > 4*newg.affinity()
}

// testInterface tests that the provided interface has an assigned IP address
// and is functional.
func testInterface(iface string) bool {
	vlog.VI(1).Infof("Testing interface %v", iface)
	ip, err := gatewayIP(iface)
	if err != nil {
		vlog.VI(1).Infof("couldn't get gateway IP for iface %q: %v", iface, err)
		return false
	}

	rtt, err := ping(ip)
	if err != nil || len(rtt) == 0 {
		vlog.VI(1).Infof("interface %q has no connectivity", iface)
		return false
	}
	return true
}

// gatewayIP finds the address on "the other side" of the provided interface.
func gatewayIP(iface string) (string, error) {
	i, err := net.InterfaceByName(iface)
	if err != nil {
		return "", fmt.Errorf("couldn't find interface %q: %v", iface, err)
	}
	addrs, err := i.Addrs()
	if err != nil {
		return "", fmt.Errorf("couldn't get addresses for interface %q: %v", iface, err)
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("interface %q has no addresses", iface)
	}
	var ip net.IP
	for _, addr := range addrs {
		if ipa, ok := addr.(*net.IPAddr); ok {
			if ip4 := ipa.IP.To4(); ip4 != nil {
				ip = ip4
				break
			}
		}
	}
	if ip == nil {
		return "", fmt.Errorf("no IP4 address for interface %q", iface)
	}
	return strings.Join(append(strings.Split(ip.String(), ".")[:3], "1"), "."), nil
}

func rttFromNames(names []string) (time.Duration, bool) {
	for _, name := range names {
		if rtt, err := rttFromName(name); err == nil {
			return rtt, true
		}
	}
	return 0, false
}
