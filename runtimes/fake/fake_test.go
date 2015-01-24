package fake_test

import (
	"testing"

	"v.io/core/veyron2"

	_ "v.io/core/veyron/profiles/fake"
)

// Ensure that the fake profile can be used to initialize a fake runtime.
func TestInit(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	if !ctx.Initialized() {
		t.Errorf("Got uninitialized context from Init.")
	}
}
