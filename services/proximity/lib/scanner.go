package proximity

import (
	"fmt"
	"net"
	"time"

	"veyron/lib/bluetooth"
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

type BluetoothScanner struct {
	device *bluetooth.Device
	c      chan ScanReading
}

func (s *BluetoothScanner) StartScan(scanInterval, scanWindow time.Duration) (<-chan ScanReading, error) {
	if s.device != nil || s.c != nil {
		return nil, fmt.Errorf("scan already in progress")
	}
	var err error
	s.device, err = bluetooth.OpenFirstAvailableDevice()
	if err != nil {
		return nil, fmt.Errorf("couldn't find an available bluetooth device: %v", err)
	}
	bc, err := s.device.StartScan(scanInterval, scanWindow)
	if err != nil {
		return nil, fmt.Errorf("couldn't start bluetooth scan: %v", err)
	}
	s.c = make(chan ScanReading, 10)
	go func() {
		for r := range bc {
			s.c <- ScanReading{
				Name:     r.Name,
				MAC:      r.MAC,
				Distance: r.Distance,
				Time:     r.Time,
			}
		}
	}()
	return s.c, nil
}

func (s *BluetoothScanner) StopScan() error {
	if s.device == nil || s.c == nil {
		if s.device != nil {
			s.device.StopScan()
			s.device.Close()
			s.device = nil
		} else {
			close(s.c)
			s.c = nil
		}
		return fmt.Errorf("scan not in progress")
	}
	s.device.StopScan()
	s.device.Close()
	s.device = nil
	close(s.c)
	s.c = nil
	return nil
}
