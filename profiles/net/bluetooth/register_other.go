// +build !veyronbluetooth
package bluetooth

func registerBT() {
}

func (p *profile) String() string {
	return "net/(no bluetooth) " + p.Platform().String()
}
