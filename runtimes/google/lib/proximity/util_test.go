package proximity

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type mockScanner struct {
	readings []ScanReading
	c        chan ScanReading
}

func (s *mockScanner) StartScan(_, _ time.Duration) (<-chan ScanReading, error) {
	s.c = make(chan ScanReading, len(s.readings))
	for _, r := range s.readings {
		s.c <- r
	}
	return s.c, nil
}

func (s *mockScanner) StopScan() error {
	close(s.c)
	return nil
}

type mockAdvertiser struct {
	lock    sync.RWMutex
	payload string
	c       chan string
	done    chan bool
}

func newMockAdvertiser() (Advertiser, <-chan string) {
	c := make(chan string)
	return &mockAdvertiser{
		c:    c,
		done: make(chan bool),
	}, c
}

func (a *mockAdvertiser) StartAdvertising(interval time.Duration) error {
	go func() {
		defer close(a.c)
		for _ = range time.Tick(interval) {
			select {
			case <-a.done:
				return
			default:
			}
			a.lock.RLock()
			p := a.payload
			a.lock.RUnlock()
			a.c <- p
		}
	}()
	return nil
}

func (a *mockAdvertiser) SetAdvertisingPayload(payload string) error {
	a.lock.Lock()
	a.payload = payload
	a.lock.Unlock()
	return nil
}

func (a *mockAdvertiser) StopAdvertising() error {
	close(a.done)
	for _ = range <-a.c {
	}
	return nil
}

func mac(id int) net.HardwareAddr {
	if id >= 256 {
		panic(fmt.Sprintf("id %d too large", id))
	}
	addr, err := net.ParseMAC(fmt.Sprintf("00:00:00:00:00:%02x", id))
	if err != nil {
		panic(fmt.Sprintf("can't create MAC address for id %d: %v", id, err))
	}
	return addr
}
