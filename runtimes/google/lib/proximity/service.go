package proximity

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"veyron/runtimes/google/lib/unit"
	"veyron2/ipc"
	prox "veyron2/services/proximity"
	"veyron2/vlog"
)

const (
	// maxRegisteredNames denotes the maximum number of names that can be
	// actively registered for this device.
	maxRegisteredNames = 10
	// advInterval specifies the frequency at which the advertising packets
	// are sent.
	advInterval = 32 * time.Millisecond
	// advCycleInterval specifies the frequency at which we change the
	// currently advertised name (out of at most maxRegisteredNames names).
	advCycleInterval = 4 * advInterval
	// scanInterval splits the entire scan duration into intervals of
	// provided length.
	scanInterval = 32 * time.Millisecond
	// scanWindow specifies, for each scanInterval, the duration during
	// which the scan will be ON.  (For the remainder of scanInterval,
	// scan will be OFF.)
	scanWindow = 16 * time.Millisecond
	// minHistoryWindow denotes the minimum time window into the past
	// beyond which proximity readings should be ignored.  In order to
	// catch all unique remote names (i.e., at most maxRegisteredNames of
	// them), we set this to double the interval at which the advertiser
	// can cycle through all the names.
	minHistoryWindow = 2 * maxRegisteredNames * advCycleInterval
)

// New returns a new instance of proximity service, given the provided
// advertiser and scanner (e.g., Bluetooth). historyWindow denotes the time
// window into the past beyond which proximity readings should be ignored.
// refreshFrequency specifies how often the list of nearby devices should be
// refreshed - shorter time duration means that the list will be more
// up-to-date but more resources will be consumed.  If the service cannot be
// created, an error is returned.
func New(a Advertiser, s Scanner, historyWindow, refreshFrequency time.Duration) (*service, error) {
	// Start advertising.
	if err := a.StartAdvertising(advInterval); err != nil {
		return nil, err
	}

	// Start scanning.
	r, err := s.StartScan(scanInterval, scanWindow)
	if err != nil {
		return nil, err
	}

	// If the history window is too small, update it.
	if historyWindow < minHistoryWindow {
		historyWindow = minHistoryWindow
	}
	srv := &service{
		devices:          make(map[string]*device),
		names:            make(map[string]int),
		advertiser:       a,
		scanner:          s,
		window:           historyWindow,
		freq:             refreshFrequency,
		readChan:         r,
		updateChan:       make(chan bool),
		updateTickerChan: time.Tick(refreshFrequency),
		advDoneChan:      make(chan bool),
	}
	go srv.readLoop()
	go srv.updateLoop()
	go srv.advLoop()
	return srv, nil
}

// device represents one neighborhood device.  It contains that device's MAC
// address, average distance, list of recent scan readings, and a list of all
// unique names stored in those scan readings.
type device struct {
	lock     sync.Mutex
	mac      net.HardwareAddr
	distance unit.Distance
	names    []string
	readings []ScanReading
}

// service maintains a list of devices in our close proximity, using scan
// readings returned by the Scanner.  It implements the ProximityService
// interface, generated from proximity.idl file.
type service struct {
	deviceLock sync.RWMutex
	nameLock   sync.RWMutex
	devices    map[string]*device
	nearby     []prox.Device
	names      map[string]int

	advertiser       Advertiser
	scanner          Scanner
	window           time.Duration
	freq             time.Duration
	readChan         <-chan ScanReading
	updateChan       chan bool
	updateTickerChan <-chan time.Time
	advDoneChan      chan bool
}

func (s *service) RegisterName(_ ipc.ServerContext, name string) error {
	s.nameLock.Lock()
	defer s.nameLock.Unlock()
	if v, ok := s.names[name]; ok {
		s.names[name] = v + 1
		return nil
	}
	if len(s.names) >= maxRegisteredNames {
		return fmt.Errorf("too many unique registered names, max allowed is %d", maxRegisteredNames)
	}
	s.names[name] = 1
	return nil
}

func (s *service) UnregisterName(_ ipc.ServerContext, name string) error {
	s.nameLock.Lock()
	defer s.nameLock.Unlock()
	v, ok := s.names[name]
	if !ok {
		return fmt.Errorf("name %q not registered", name)
	}
	if v <= 1 {
		delete(s.names, name)
	} else {
		s.names[name] = v - 1
	}
	return nil
}

// NearbyDevices returns the list of nearby devices, sorted in increasing
// distance order.
func (s *service) NearbyDevices(_ ipc.ServerContext) ([]prox.Device, error) {
	s.deviceLock.RLock()
	defer s.deviceLock.RUnlock()
	return s.nearby, nil
}

// Stop terminates the process of gathering proximity information, returning
// any error encountered.
func (s *service) Stop() {
	vlog.VI(1).Info("stopping proximity service")
	// Stop the scanner.  This action will cause the following cascading
	// effect: readChan closed -> readLoop terminated -> updateChan closed
	// -> updateLoop terminated.
	s.scanner.StopScan()
	// Stop the advertiser.
	s.advertiser.StopAdvertising()
	// Close advDoneChan, which will terminate advLoop.
	close(s.advDoneChan)
}

// readLoop extracts readings from readChan and writes notifications to
// updateChan.
func (s *service) readLoop() {
	for r := range s.readChan {
		s.deviceLock.Lock()
		d := s.devices[r.MAC.String()]
		if d == nil {
			d = &device{
				mac: r.MAC,
			}
			s.devices[d.mac.String()] = d
		}
		d.lock.Lock()
		d.readings = append(d.readings, r)
		d.lock.Unlock()
		s.deviceLock.Unlock()

		// Notify.  We want at most one outstanding notification but
		// don't want to block.
		select {
		case s.updateChan <- true:
		default:
		}
	}
	close(s.updateChan)
	vlog.VI(1).Info("proximity service's read goroutine exiting")
}

// updateLoop periodically updates the state of nearby devices.
func (s *service) updateLoop() {
	defer vlog.VI(1).Info("proximity service's update goroutine exiting")
	for {
		// Wait for a ticker.  The ticker helps us avoid wasting
		// resources by doing too-frequent updates. If either the ticker
		// or the update channel gets closed in the meantime, we exit
		// this goroutine.
		var exit bool
		for !exit {
			select {
			case _, ok := <-s.updateTickerChan:
				if !ok {
					return
				}
				exit = true
			case _, ok := <-s.updateChan:
				if !ok {
					return
				}
			}
		}

		s.updateNearbyState()
	}
}

// advLoop cycles through all registered names and advertises each one
// for a specified time interval (advCycleInterval).  It listens on advDoneChan
// and terminates when something is sent on it or when it gets closed.
func (s *service) advLoop() {
	tickerChan := time.Tick(advCycleInterval)
	var ns []string
	var idx int
	defer vlog.VI(1).Info("proximity service's advertising goroutine exiting")
	for {
		select {
		case <-s.advDoneChan:
			return
		case <-tickerChan:
		}

		if idx >= len(ns) {
			// Cycled once through all copied names - create a
			// new copy of registered names.
			s.nameLock.RLock()
			ns = nil
			for key := range s.names {
				ns = append(ns, key)
			}
			s.nameLock.RUnlock()
			idx = 0
			if len(ns) == 0 {
				// No names to advertise: advertise an empty
				// string so that neighbors will at least
				// know this device exists.
				ns = append(ns, "")
			}
		}
		name := ns[idx]
		if err := s.advertiser.SetAdvertisingPayload(name); err != nil {
			vlog.Errorf("couldn't set advertising payload %s: %v", name, err)
		}
		idx++
	}
}

// updateNearbyStates re-computes the neighborhood list using the most recent
// scan readings and updates it in-place.
func (s *service) updateNearbyState() {
	// Reject all readings with timestamps before this barrier.
	barrier := time.Now().Add(-1 * s.window)

	// Get devices with recent readings and purge the rest.
	var recent []*device
	s.deviceLock.Lock()
	for _, d := range s.devices {
		d.lock.Lock()
		if len(d.readings) == 0 || d.readings[len(d.readings)-1].Time.Before(barrier) { // no recent readings.
			delete(s.devices, d.mac.String())
		} else {
			recent = append(recent, d)
		}
		d.lock.Unlock()
	}
	s.deviceLock.Unlock()

	// Purge stale readings from remaining devices and compute average
	// proximity.
	devices := make([]device, 0, len(recent))
	for _, d := range recent {
		d.lock.Lock()
		// Find the index at which readings become stale.
		var idx int
		for idx = len(d.readings) - 1; idx >= 0; idx-- {
			if r := d.readings[idx]; r.Time.Before(barrier) {
				break
			}
		}
		// Remove stale readings.
		d.readings = d.readings[idx+1:]

		// If we have a non-infinite distance, add the device to our
		// list.
		if dist := avgDistance(d.readings); dist != unit.MaxDistance {
			devices = append(devices, device{
				names:    uniqueNames(d.readings),
				mac:      d.mac,
				distance: dist,
			})
		}
		d.lock.Unlock()
	}

	// Sort all devices by proximity.
	incDistance := func(d1, d2 device) bool {
		return d1.distance < d2.distance
	}
	sort.Sort(&deviceSorter{
		devices: devices,
		by:      incDistance,
	})

	// Update device list.
	newDevs := make([]prox.Device, len(devices))
	for i, d := range devices {
		// Sort names just for stability.
		sort.Strings(d.names)
		newDevs[i] = prox.Device{
			Names:    d.names,
			MAC:      d.mac.String(),
			Distance: d.distance.String(),
		}
	}

	vlog.VI(1).Info("Nearby devices:", newDevs)

	s.deviceLock.Lock()
	s.nearby = newDevs
	s.deviceLock.Unlock()
}

func avgDistance(readings []ScanReading) unit.Distance {
	if len(readings) == 0 {
		return unit.MaxDistance
	}
	// Ignore the smallest and largest 33% of the readings.
	decRSSI := func(r1, r2 ScanReading) bool {
		return r1.Distance < r2.Distance
	}
	sort.Sort(&readingSorter{
		readings: readings,
		by:       decRSSI,
	})
	trim := len(readings) / 3
	readings = readings[trim : len(readings)-trim]

	// Prune all readings with unit.MaxDistance distance.
	idx := len(readings) - 1
	for ; idx >= 0 && readings[idx].Distance == unit.MaxDistance; idx-- {
	}
	readings = readings[:idx+1]

	if len(readings) == 0 {
		return unit.MaxDistance
	}

	// Compute average distance.
	var totalDistance unit.Distance
	for _, r := range readings {
		totalDistance += r.Distance
	}
	return totalDistance / unit.Distance(len(readings))
}

func uniqueNames(readings []ScanReading) []string {
	ns := make(map[string]bool)
	for _, r := range readings {
		ns[r.Name] = true
	}
	var names []string
	for name := range ns {
		names = append(names, name)
	}
	return names
}
