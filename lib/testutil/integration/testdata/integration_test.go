package testdata

import (
	"testing"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil/integration"
	_ "v.io/core/veyron/profiles"
)

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestBinaryFromPath(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	bash := env.BinaryFromPath("/bin/bash")
	if want, got := "hello world\n", bash.Start("-c", "echo hello world").Output(); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}

	// TODO(sjr): revive this once stderr handling is fixed.
	inv := bash.Start("-c", "echo hello world 1>&2")
	inv.WaitOrDie(nil, nil)
	if want, got := "hello world\n", inv.ErrorOutput(); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}
}
