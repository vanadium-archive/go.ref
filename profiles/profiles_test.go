package profiles_test

import (
	"os"
	"testing"

	"v.io/veyron/veyron/profiles"
)

func TestGeneric(t *testing.T) {
	p := profiles.New()

	if got, want := p.Name(), "generic"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	hostname, _ := os.Hostname()
	if got, want := p.Platform().Node, hostname; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
