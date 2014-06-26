// +build !linux

package proximity

// NewBluetoothScanner returns a bluetooth Scanner instance.
func NewBluetoothScanner() (Scanner, error) {
	return nil, fmt.Errorf("bluetooth scanner not yet supported")
}
