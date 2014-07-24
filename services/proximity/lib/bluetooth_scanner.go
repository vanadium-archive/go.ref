package proximity

import (
	"fmt"
	"time"

	"veyron/lib/bluetooth"
)

// NewBluetoothScanner returns a Scanner instance that uses Low-Energy Bluetooth to scan
// for nearby devices.
func NewBluetoothScanner() (Scanner, error) {
	return &bluetoothScanner{}, nil
}

type bluetoothScanner struct {
	device *bluetooth.Device
	c      chan ScanReading
}

func (s *bluetoothScanner) StartScan(scanInterval, scanWindow time.Duration) (<-chan ScanReading, error) {
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

func (s *bluetoothScanner) StopScan() error {
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
