package proximity

import (
	"net"
	"time"

	"veyron/lib/unit"
)

// Scanner denotes a (local) entity that is capable of scanning for nearby
// devices (e.g., Bluetooth).
type Scanner interface {
	// StartScan initiates a scan on the local device.  The scan will
	// proceed over many duration intervals; within each interval, scan will
	// be ON only for a given duration window.  All scan readings
	// encountered during scan-ON periods will be pushed onto the returned
	// channel.  If the scan cannot be started, an error is returned.
	StartScan(scanInterval, scanWindow time.Duration) (<-chan ScanReading, error)
	// StopScan stops any scan in progress on the local device, closing
	// the channel returned by the previous call to StartScan().
	// If the device is not scanning, this function will be a noop.
	StopScan() error
}

// ScanReading holds a single reading of a neighborhood device's advertisement
// during a scan.  Typically, there will be many such readings for a particular
// neighborhood device.
type ScanReading struct {
	// Name represents a local name of the remote device.  It can also store
	// arbitrary application-specific data.
	Name string
	// MAC is the hardware address of the remote device.
	MAC net.HardwareAddr
	// Distance represents the (estimated) distance to the neighborhood
	// device.
	Distance unit.Distance
	// Time is the time the advertisement packed was received/scanned.
	Time time.Time
}
