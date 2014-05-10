package rt_test

import (
	"fmt"
	"net"

	"veyron/runtimes/google/rt"
	"veyron2"
	"veyron2/security"
)

func ExampleInit() {
	r, err := rt.New()
	if err != nil {
		fmt.Printf("rt.New failed: %q\n", err)
		return
	}
	product := r.Product()
	vendor, model, _ := product.Description()
	fmt.Printf("hello world from %q, model: %q\n", vendor, model)
	// Output:
	// hello world from "google", model: "raspberry_pi"
}

type myNewProduct struct{}

func (m *myNewProduct) Description() (vendor, model, name string) {
	return "acme", "goobie", "one"
}

func (m *myNewProduct) ID() security.PublicID {
	vendor, model, name := m.Description()
	return security.FakePublicID(fmt.Sprintf("%s/%s/%s", vendor, model, name))
}

func (m *myNewProduct) Addresses() []net.Addr {
	return []net.Addr{}
}

func ExampleInitForProduct() {
	r, err := rt.New(veyron2.ProductOpt{&myNewProduct{}})
	if err != nil {
		fmt.Printf("rt.New failed: %q\n", err)
		return
	}
	product := r.Product()
	vendor, model, _ := product.Description()
	fmt.Printf("hello world from %q, model: %q\n", vendor, model)
	// Output:
	// hello world from "acme", model: "goobie"
}

// Demonstrate how an implementation specific option would be defined
// and used. The actual example below will generate a run-time error
// since this option is not actually implemented.
type MyImplementationOpt struct{}

// Implement ROpt
func (*MyImplementationOpt) ROpt() {}

func ExampleInitWithImplementationOption() {
	_, err := rt.New(&MyImplementationOpt{})
	if err != nil {
		fmt.Printf("%s\n", err)
	}
	// Output:
	// option has wrong type *rt_test.MyImplementationOpt
}
