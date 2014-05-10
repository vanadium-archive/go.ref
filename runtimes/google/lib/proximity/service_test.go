package proximity

import (
	"reflect"
	"testing"
	"time"

	"veyron/runtimes/google/lib/unit"
	"veyron2/services/proximity"
)

type testStream struct {
	c chan<- []proximity.Device
}

func (s *testStream) Send(item []proximity.Device) error {
	s.c <- item
	return nil
}

func testNearbyDevices(t *testing.T, history time.Duration, input []ScanReading, expected []proximity.Device) {
	const freq = 1 * time.Nanosecond // update as fast as possible

	// Append a dummy scan reading with a unique device name.  We will later
	// wait until we see this device in the output, indicating that all of
	// the scan readings have been processed and accounted for.
	const dummyName = "$!dummy#@"
	input = append(input, ScanReading{dummyName, mac(0), 0, time.Now()})
	adv, _ := newMockAdvertiser()
	s, err := New(adv, &mockScanner{readings: input}, history, freq)
	if err != nil {
		t.Fatalf("couldn't create proximity service: %v", err)
	}

	// Loop until we reach the expected number of devices/readings.
	for {
		devices, err := s.NearbyDevices(nil)
		if err != nil {
			t.Fatalf("error getting nearby devices: %v", err)
		}
		var done bool
		for i, d := range devices {
			if len(d.Names) == 1 && d.Names[0] == dummyName {
				// Remove dummy device from the list.
				devices = append(devices[:i], devices[i+1:]...)
				done = true
				break
			}
		}
		if done {
			if !reflect.DeepEqual(devices, expected) {
				t.Errorf("devices mismatch: got %#v, want %#v", devices, expected)
			}
			break
		}
	}

	// Stop proximity.
	s.Stop()
}

func TestNearbyDevicesUniq(t *testing.T) {
	now := time.Now()
	testNearbyDevices(t, 100*time.Hour,
		[]ScanReading{
			{"N1", mac(1), 5 * unit.Meter, now},
			{"N2", mac(2), 2 * unit.Meter, now},
			{"N3", mac(3), 1 * unit.Meter, now},
			{"N4", mac(4), 7 * unit.Meter, now},
			{"N5", mac(5), 3 * unit.Meter, now},
			{"N6", mac(6), 4 * unit.Meter, now},
			{"N7", mac(7), 6 * unit.Meter, now},
		},
		[]proximity.Device{
			{mac(3).String(), []string{"N3"}, (1 * unit.Meter).String()},
			{mac(2).String(), []string{"N2"}, (2 * unit.Meter).String()},
			{mac(5).String(), []string{"N5"}, (3 * unit.Meter).String()},
			{mac(6).String(), []string{"N6"}, (4 * unit.Meter).String()},
			{mac(1).String(), []string{"N1"}, (5 * unit.Meter).String()},
			{mac(7).String(), []string{"N7"}, (6 * unit.Meter).String()},
			{mac(4).String(), []string{"N4"}, (7 * unit.Meter).String()},
		})
}

func TestNearbyDevicesAggr(t *testing.T) {
	now := time.Now()
	testNearbyDevices(t, 100*time.Hour,
		[]ScanReading{
			{"N1", mac(1), 5 * unit.Meter, now},
			{"N2", mac(2), 2 * unit.Meter, now},
			{"N1", mac(1), 7 * unit.Meter, now},
			{"N2", mac(2), 2 * unit.Meter, now},
			{"N1", mac(1), 3 * unit.Meter, now},
			{"N2", mac(2), 5 * unit.Meter, now},
			{"N1", mac(1), 1 * unit.Meter, now},
			{"N2", mac(2), 5 * unit.Meter, now},
			{"N3", mac(3), 1 * unit.Meter, now},
		},
		[]proximity.Device{
			{mac(3).String(), []string{"N3"}, (1 * unit.Meter).String()},
			{mac(2).String(), []string{"N2"}, (3.5 * unit.Meter).String()},
			{mac(1).String(), []string{"N1"}, (4 * unit.Meter).String()},
		})
}

func TestNearbyDevicesReadingsStale(t *testing.T) {
	now := time.Now()
	oneHrAgo := now.Add(-1 * time.Hour)

	testNearbyDevices(t, 10*time.Minute,
		[]ScanReading{
			{"N1", mac(1), 5 * unit.Meter, oneHrAgo},
			{"N2", mac(2), 2 * unit.Meter, oneHrAgo},
			{"N1", mac(1), 9 * unit.Meter, oneHrAgo},
			{"N2", mac(2), 3 * unit.Meter, now},
			{"N1", mac(1), 4 * unit.Meter, now},
			{"N2", mac(2), 4 * unit.Meter, now},
			{"N3", mac(3), 1 * unit.Meter, now},
		},
		[]proximity.Device{
			{mac(3).String(), []string{"N3"}, (1 * unit.Meter).String()},
			{mac(2).String(), []string{"N2"}, (3.5 * unit.Meter).String()},
			{mac(1).String(), []string{"N1"}, (4 * unit.Meter).String()},
		})
}

func TestNearbyDevicesDevicesStale(t *testing.T) {
	now := time.Now()
	oneHrAgo := now.Add(-1 * time.Hour)

	testNearbyDevices(t, 10*time.Minute,
		[]ScanReading{
			{"N1", mac(1), 5 * unit.Meter, oneHrAgo},
			{"N2", mac(2), 2 * unit.Meter, oneHrAgo},
			{"N3", mac(3), 1 * unit.Meter, oneHrAgo},
			{"N1", mac(1), 7 * unit.Meter, now},
			{"N2", mac(2), 2 * unit.Meter, now},
			{"N1", mac(1), 4 * unit.Meter, now},
			{"N2", mac(2), 4 * unit.Meter, now},
		},
		[]proximity.Device{
			{mac(2).String(), []string{"N2"}, (3 * unit.Meter).String()},
			{mac(1).String(), []string{"N1"}, (5.5 * unit.Meter).String()},
		})
}

func TestNearbyDevicesManyNames(t *testing.T) {
	now := time.Now()
	testNearbyDevices(t, 100*time.Hour,
		[]ScanReading{
			{"N1B", mac(1), 6 * unit.Meter, now},
			{"N2A", mac(2), 2 * unit.Meter, now},
			{"N1A", mac(1), 7 * unit.Meter, now},
			{"N2A", mac(2), 2 * unit.Meter, now},
			{"N1A", mac(1), 5 * unit.Meter, now},
			{"N2B", mac(2), 5 * unit.Meter, now},
			{"N1A", mac(1), 1 * unit.Meter, now},
			{"N2B", mac(2), 5 * unit.Meter, now},
			{"N1A", mac(1), 4 * unit.Meter, now},
			{"N3", mac(3), 1 * unit.Meter, now},
		},
		[]proximity.Device{
			{mac(3).String(), []string{"N3"}, (1 * unit.Meter).String()},
			{mac(2).String(), []string{"N2A", "N2B"}, (3.5 * unit.Meter).String()},
			{mac(1).String(), []string{"N1A", "N1B"}, (5 * unit.Meter).String()},
		})
}

func TestRegisterName(t *testing.T) {
	adv, advChan := newMockAdvertiser()
	s, err := New(adv, &mockScanner{}, time.Second, time.Second)
	if err != nil {
		t.Errorf("error creating proximity service: %v", err)
	}

	// Empty string should be advertising initially.
	for name := range advChan {
		if name != "" {
			t.Errorf("got advertised name %q, expected %q", name, "")
		}
		break
	}

	// Register name and wait for it to begin advertising.
	s.RegisterName(nil, "N1")
	for name := range advChan {
		if name == "N1" {
			break
		} else if name != "" {
			t.Errorf("got advertised name %q, expected %q", name, "N1")
		}
	}

	// Register another name and wait for it to begin advertising.
	s.RegisterName(nil, "N2")
	var found1, found2 bool
Loop:
	for name := range advChan {
		switch name {
		case "N1":
			found1 = true
		case "N2":
			found2 = true
		default:
			t.Errorf("got advertised name %q, expected %q or %q", name, "N1", "N2")
			break Loop
		}
		if found1 && found2 {
			break
		}
	}

	// Unregister a name and wait for it to disappear.  We'll consider a
	// name disappearing if it doesn't show up in 30 consecutive
	// advertisements.
	s.UnregisterName(nil, "N1")
	var n int
	for name := range advChan {
		if name == "N1" {
			n = 0
		} else {
			n++
			if n >= 30 {
				break
			}
		}
	}

	// Stop service and wait for the advertising channel to be closed.
	s.Stop()
	for _ = range advChan {
	}
}
