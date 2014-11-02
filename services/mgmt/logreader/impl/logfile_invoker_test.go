package impl_test

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"veyron.io/veyron/veyron/services/mgmt/logreader/impl"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/logreader"
	"veyron.io/veyron/veyron2/services/mgmt/logreader/types"
	"veyron.io/veyron/veyron2/verror"
)

type logFileDispatcher struct {
	root string
}

func (d *logFileDispatcher) Lookup(suffix, _ string) (interface{}, security.Authorizer, error) {
	return impl.NewLogFileInvoker(d.root, suffix), nil, nil
}

func writeAndSync(t *testing.T, w *os.File, s string) {
	if _, err := w.WriteString(s); err != nil {
		t.Fatalf("w.WriteString failed: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("w.Sync failed: %v", err)
	}
}

func TestReadLogImplNoFollow(t *testing.T) {
	runtime := rt.Init()

	workdir, err := ioutil.TempDir("", "logreadertest")
	if err != nil {
		t.Fatalf("ioutil.TempDir: %v", err)
	}
	defer os.RemoveAll(workdir)
	server, endpoint, err := startServer(t, &logFileDispatcher{workdir})
	if err != nil {
		t.Fatalf("startServer failed: %v", err)
	}
	defer stopServer(t, server)

	const testFile = "mylogfile.INFO"
	writer, err := os.Create(path.Join(workdir, testFile))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	tests := []string{
		"Hello World!",
		"Life is too short",
		"Have fun",
		"Play hard",
		"Break something",
		"Fix it later",
	}
	for _, s := range tests {
		writeAndSync(t, writer, s+"\n")
	}

	// Try to access a file that doesn't exist.
	lf, err := logreader.BindLogFile(naming.JoinAddressName(endpoint, "//doesntexist"))
	if err != nil {
		t.Errorf("BindLogFile: %v", err)
	}
	_, err = lf.Size(runtime.NewContext())
	if expected := verror.NoExist; !verror.Is(err, expected) {
		t.Errorf("unexpected error value, got %v, want: %v", err, expected)
	}

	// Try to access a file that does exist.
	lf, err = logreader.BindLogFile(naming.JoinAddressName(endpoint, "//"+testFile))
	if err != nil {
		t.Errorf("BindLogFile: %v", err)
	}
	_, err = lf.Size(runtime.NewContext())
	if err != nil {
		t.Errorf("Size failed: %v", err)
	}

	// Read without follow.
	stream, err := lf.ReadLog(runtime.NewContext(), 0, types.AllEntries, false)
	if err != nil {
		t.Errorf("ReadLog failed: %v", err)
	}
	rStream := stream.RecvStream()
	expectedPosition := int64(0)
	for count := 0; rStream.Advance(); count++ {
		entry := rStream.Value()
		if entry.Position != expectedPosition {
			t.Errorf("unexpected position. Got %v, want %v", entry.Position, expectedPosition)
		}
		if expected := tests[count]; entry.Line != expected {
			t.Errorf("unexpected content. Got %q, want %q", entry.Line, expected)
		}
		expectedPosition += int64(len(entry.Line)) + 1
	}

	if err := rStream.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", rStream.Err())
	}
	offset, err := stream.Finish()
	if err != nil {
		t.Errorf("Finish failed: %v", err)
	}
	if offset != expectedPosition {
		t.Errorf("unexpected offset. Got %q, want %q", offset, expectedPosition)
	}

	// Read with follow from EOF (where the previous read ended).
	stream, err = lf.ReadLog(runtime.NewContext(), offset, types.AllEntries, false)
	if err != nil {
		t.Errorf("ReadLog failed: %v", err)
	}
	_, err = stream.Finish()
	if !verror.Is(err, types.EOF) {
		t.Errorf("unexpected error, got %#v, want EOF", err)
	}
}

func TestReadLogImplWithFollow(t *testing.T) {
	runtime := rt.Init()

	workdir, err := ioutil.TempDir("", "logreadertest")
	if err != nil {
		t.Fatalf("ioutil.TempDir: %v", err)
	}
	defer os.RemoveAll(workdir)
	server, endpoint, err := startServer(t, &logFileDispatcher{workdir})
	if err != nil {
		t.Fatalf("startServer failed: %v", err)
	}
	defer stopServer(t, server)

	const testFile = "mylogfile.INFO"
	writer, err := os.Create(path.Join(workdir, testFile))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	tests := []string{
		"Hello World!",
		"Life is too short",
		"Have fun",
		"Play hard",
		"Break something",
		"Fix it later",
	}

	lf, err := logreader.BindLogFile(naming.JoinAddressName(endpoint, "//"+testFile))
	if err != nil {
		t.Errorf("BindLogFile: %v", err)
	}
	_, err = lf.Size(runtime.NewContext())
	if err != nil {
		t.Errorf("Size failed: %v", err)
	}

	// Read with follow.
	stream, err := lf.ReadLog(runtime.NewContext(), 0, int32(len(tests)), true)
	if err != nil {
		t.Errorf("ReadLog failed: %v", err)
	}
	rStream := stream.RecvStream()
	writeAndSync(t, writer, tests[0]+"\n")
	for count, pos := 0, int64(0); rStream.Advance(); count++ {
		entry := rStream.Value()
		if entry.Position != pos {
			t.Errorf("unexpected position. Got %v, want %v", entry.Position, pos)
		}
		if expected := tests[count]; entry.Line != expected {
			t.Errorf("unexpected content. Got %q, want %q", entry.Line, expected)
		}
		pos += int64(len(entry.Line)) + 1
		if count+1 < len(tests) {
			writeAndSync(t, writer, tests[count+1]+"\n")
		}
	}

	if err := rStream.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", rStream.Err())
	}
	_, err = stream.Finish()
	if err != nil {
		t.Errorf("Finish failed: %v", err)
	}
}
