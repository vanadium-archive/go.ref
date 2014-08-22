package impl_test

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"sort"
	"testing"

	"veyron/services/mgmt/logreader/impl"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mounttable"
	"veyron2/verror"
)

type logDirectoryDispatcher struct {
	root string
}

func (d *logDirectoryDispatcher) Lookup(suffix, _ string) (ipc.Invoker, security.Authorizer, error) {
	return impl.NewLogDirectoryInvoker(d.root, suffix), nil, nil
}

func TestLogDirectory(t *testing.T) {
	runtime := rt.Init()

	workdir, err := ioutil.TempDir("", "logreadertest")
	if err != nil {
		t.Fatalf("ioutil.TempDir: %v", err)
	}
	defer os.RemoveAll(workdir)
	server, endpoint, err := startServer(t, &logDirectoryDispatcher{workdir})
	if err != nil {
		t.Fatalf("startServer failed: %v", err)
	}
	defer stopServer(t, server)

	if err := os.Mkdir(path.Join(workdir, "subdir"), os.FileMode(0755)); err != nil {
		t.Fatalf("os.Mkdir failed: %v", err)
	}
	tests := []string{
		"foo.txt",
		"bar.txt",
		"subdir/foo2.txt",
		"subdir/bar2.txt",
	}
	for _, s := range tests {
		if err := ioutil.WriteFile(path.Join(workdir, s), []byte(s), os.FileMode(0644)); err != nil {
			t.Fatalf("ioutil.WriteFile failed: %v", err)
		}
	}

	// Try to access a directory that doesn't exist.
	ld, err := mounttable.BindGlobbable(naming.JoinAddressName(endpoint, "//doesntexist"))
	if err != nil {
		t.Errorf("BindLogDirectory: %v", err)
	}
	stream, err := ld.Glob(runtime.NewContext(), "*")
	if err != nil {
		t.Errorf("unexpected error, got %v, want: nil", err)
	}
	if err := stream.Finish(); err != nil {
		if expected := verror.NotFound; !verror.Is(err, expected) {
			t.Errorf("unexpected error value, got %v, want: %v", err, expected)
		}
	}

	// Try to access a directory that does exist.
	ld, err = mounttable.BindGlobbable(naming.JoinAddressName(endpoint, "//"))
	if err != nil {
		t.Errorf("BindLogDirectory: %v", err)
	}

	stream, err = ld.Glob(runtime.NewContext(), "...")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	results := []string{}
	iterator := stream.RecvStream()
	for count := 0; iterator.Advance(); count++ {
		results = append(results, iterator.Value().Name)
	}
	sort.Strings(tests)
	sort.Strings(results)
	if !reflect.DeepEqual(tests, results) {
		t.Errorf("unexpected result. Got %v, want %v", results, tests)
	}

	if err := iterator.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", iterator.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Finish failed: %v", err)
	}

	stream, err = ld.Glob(runtime.NewContext(), "*")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	results = []string{}
	iterator = stream.RecvStream()
	for count := 0; iterator.Advance(); count++ {
		results = append(results, iterator.Value().Name)
	}
	sort.Strings(results)
	expected := []string{
		"bar.txt",
		"foo.txt",
	}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("unexpected result. Got %v, want %v", results, expected)
	}

	if err := iterator.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", iterator.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Finish failed: %v", err)
	}

	stream, err = ld.Glob(runtime.NewContext(), "subdir/*")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	results = []string{}
	iterator = stream.RecvStream()
	for count := 0; iterator.Advance(); count++ {
		results = append(results, iterator.Value().Name)
	}
	sort.Strings(results)
	expected = []string{
		"subdir/bar2.txt",
		"subdir/foo2.txt",
	}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("unexpected result. Got %v, want %v", results, expected)
	}

	if err := iterator.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", iterator.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Finish failed: %v", err)
	}

	ld, err = mounttable.BindGlobbable(naming.JoinAddressName(endpoint, "//subdir"))
	if err != nil {
		t.Errorf("BindLogDirectory: %v", err)
	}
	stream, err = ld.Glob(runtime.NewContext(), "*")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	results = []string{}
	iterator = stream.RecvStream()
	for count := 0; iterator.Advance(); count++ {
		results = append(results, iterator.Value().Name)
	}
	sort.Strings(results)
	expected = []string{
		"bar2.txt",
		"foo2.txt",
	}
	if !reflect.DeepEqual(expected, results) {
		t.Errorf("unexpected result. Got %v, want %v", results, expected)
	}

	if err := iterator.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", iterator.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Finish failed: %v", err)
	}
}
