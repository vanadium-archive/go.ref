// Package gateway implements client and server methods that (1) allow a device
// to act as an IP gateway and (2) allow devices without an IP connection to
// obtain it via a gateway device.
//
// The main gateway functionality is achieved through the Bluetooth BNEP
// protocol.  This protocol allows a virtual Ethernet connection to be
// established between multiple Bluetooth devices.  We plugin the remaining
// functionality (IP forwarding, DHCP) using standard Linux tools (e.g.,
// route/dhcpd).  The current implementation is therefore platform-specific and
// for now only supported on Linux.
//
// For the Bluetooth stack, we use Bluez 4.101 library along with standard
// linux Bluetooth drivers.  As Bluez libraries go through extensive
// non-backward-compatible changes, we require that this exact library version
// is installed on target systems.  We use DBus to communicate with the Bluez
// library.
//
// To configure the IP stack we use a number of standard Linux tools, namely:
// awk, grep, cut, route, dhcpcd, and dhcpd.  These tools must be installed on
// target systems and must be present in system's $PATH environmental variable.
// We also rely on IP4 addressing and must revisit the implementation should
// IPv6 become the only standard.
package gateway
