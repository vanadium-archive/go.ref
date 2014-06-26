// +build !linux

package proximity

import "fmt"

// NewBluetoothScanner always returns (nil, <error>) as bluetooth scanners
// are not supported on this platform.
func NewBluetoothScanner() (Scanner, error) {
	return nil, fmt.Errorf("bluetooth scanner not yet supported")
}
