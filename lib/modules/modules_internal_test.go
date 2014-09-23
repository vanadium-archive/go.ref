package modules

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"testing"
)

func init() {
	RegisterChild("echos", Echo)
}

func Echo(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("no args")
	}
	for _, a := range args {
		fmt.Println(a)
	}
	return nil
}

func assertNumHandles(t *testing.T, sh *Shell, n int) {
	if got, want := len(sh.handles), n; got != want {
		_, file, line, _ := runtime.Caller(2)
		t.Errorf("%s:%d: got %d, want %d", filepath.Base(file), line, got, want)
	}
}

func TestState(t *testing.T) {
	sh := NewShell()

	sh.AddSubprocess("echonotregistered", "[args]*")
	sh.AddSubprocess("echos", "[args]*")
	sh.AddFunction("echof", Echo, "[args]*")
	assertNumHandles(t, sh, 0)

	_, _ = sh.Start("echonotregistered") // won't start.
	hs, _ := sh.Start("echos", "a")
	hf, _ := sh.Start("echof", "b")
	assertNumHandles(t, sh, 2)

	for i, h := range []Handle{hs, hf} {
		if got := h.Shutdown(nil); got != nil {
			t.Errorf("%d: got %q, want %q", i, got, nil)
		}
	}
	assertNumHandles(t, sh, 0)

	hs, _ = sh.Start("echos", "a", "b")
	hf, _ = sh.Start("echof", "c")
	assertNumHandles(t, sh, 2)

	sh.Cleanup(nil)
	assertNumHandles(t, sh, 0)
}
