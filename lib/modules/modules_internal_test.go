package modules

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"testing"
)

func Echo(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("no args")
	}
	for _, a := range args {
		fmt.Fprintln(stdout, a)
	}
	return nil
}

func assertNumHandles(t *testing.T, sh *Shell, n int) {
	if got, want := len(sh.handles), n; got != want {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s:%d: got %d, want %d", filepath.Base(file), line, got, want)
	}
}

func TestState(t *testing.T) {
	sh, err := NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	RegisterFunction("echoff", "[args]*", Echo)
	assertNumHandles(t, sh, 0)

	_, _ = sh.Start("echonotregistered", nil) // won't start.
	hs, _ := sh.Start("echos", nil, "a")
	hf, _ := sh.Start("echoff", nil, "b")
	assertNumHandles(t, sh, 2)

	for i, h := range []Handle{hs, hf} {
		if got := h.Shutdown(nil, nil); got != nil {
			t.Errorf("%d: got %q, want %v", i, got, nil)
		}
	}
	assertNumHandles(t, sh, 0)

	hs, _ = sh.Start("echos", nil, "a", "b")
	hf, _ = sh.Start("echof", nil, "c")
	assertNumHandles(t, sh, 2)

	sh.Cleanup(nil, nil)
	assertNumHandles(t, sh, 0)
}
