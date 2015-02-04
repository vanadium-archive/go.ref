package testdata

import (
	"bufio"
	"bytes"
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

	inv := bash.Start("-c", "echo hello world 1>&2")
	var buf bytes.Buffer
	inv.WaitOrDie(nil, bufio.NewWriter(&buf))
	if want, got := "hello world\n", string(buf.Bytes()); want != got {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}
}
