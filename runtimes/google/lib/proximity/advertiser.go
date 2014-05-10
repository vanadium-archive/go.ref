package proximity

import "time"

// Advertiser denotes a (local) entity that is capable of advertising small
// payloads to neighboring devices (e.g., Bluetooth).
type Advertiser interface {
	// StartAdvertising initiates the process of sending advertising packets
	// after every tick of the provided time interval.  The payload sent
	// with each advertising packet can be specified via
	// SetAdvertisingPayload.
	// This method may be called again even if advertising is currently
	// enabled, in order to adjust the advertising interval.
	StartAdvertising(advInterval time.Duration) error

	// SetAdvertisingPayload sets the advertising payload that is sent with
	// each advertising packet.  This function may be called at any time to
	// adjust the payload that is currently being advertised.
	SetAdvertisingPayload(payload string) error

	// StopAdvertising stops advertising.  If the device is not advertising,
	// this function will be a noop.
	StopAdvertising() error
}
