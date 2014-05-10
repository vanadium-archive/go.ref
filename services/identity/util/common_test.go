package util

import "net/http"

type iface interface {
	Method()
}
type impl struct{ Content string }

func (i *impl) Method() {}

var _ iface = (*impl)(nil)

func newRequest() *http.Request {
	r, err := http.NewRequest("GET", "http://does-not-matter", nil)
	if err != nil {
		panic(err)
	}
	return r
}
