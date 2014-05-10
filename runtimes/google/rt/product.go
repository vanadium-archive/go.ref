package rt

import (
	"fmt"
	"net"
	"os"

	"veyron2/product"
	"veyron2/security"
)

func (r *vrt) Product() product.T {
	return r.product
}

type googleProducts struct{}

func (g *googleProducts) Description() (vendor, product, name string) {
	host, _ := os.Hostname()
	return "google", "generic", host
}

func (g *googleProducts) Identity() security.PublicID {
	v, p, n := g.Description()
	return security.FakePublicID(fmt.Sprintf("%s/%s/%s", v, p, n))
}

func (g *googleProducts) Addresses() []net.Addr {
	return []net.Addr{}
}
