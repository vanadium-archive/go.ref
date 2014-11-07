package impl_test

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"sort"
	"testing"

	"veyron.io/veyron/veyron/services/mgmt/logreader/impl"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mounttable"
	"veyron.io/veyron/veyron2/verror"
)

type logDirectoryDispatcher struct {
	root string
}

func (d *logDirectoryDispatcher) Lookup(suffix, _ string) (interface{}, security.Authorizer, error) {
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
	{
		ld := mounttable.GlobbableClient(naming.JoinAddressName(endpoint, "//doesntexist"))
		stream, err := ld.Glob(runtime.NewContext(), "*")
		if err != nil {
			t.Errorf("unexpected error, got %v, want: nil", err)
		}
		if err := stream.Finish(); err != nil {
			if expected := verror.NoExist; !verror.Is(err, expected) {
				t.Errorf("unexpected error value, got %v, want: %v", err, expected)
			}
		}
	}

	// Try to access a directory that does exist.
	{
		ld := mounttable.GlobbableClient(naming.JoinAddressName(endpoint, "//"))
		stream, err := ld.Glob(runtime.NewContext(), "...")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results := []string{}
		expected := []string{"", "subdir"}
		expected = append(expected, tests...)
		iterator := stream.RecvStream()
		for count := 0; iterator.Advance(); count++ {
			results = append(results, iterator.Value().Name)
		}
		sort.Strings(expected)
		sort.Strings(results)
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
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
		expected = []string{
			"bar.txt",
			"foo.txt",
			"subdir",
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
	}

	// Try to access a sub-directory.
	{
		ld := mounttable.GlobbableClient(naming.JoinAddressName(endpoint, "//subdir"))
		stream, err := ld.Glob(runtime.NewContext(), "*")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results := []string{}
		iterator := stream.RecvStream()
		for count := 0; iterator.Advance(); count++ {
			results = append(results, iterator.Value().Name)
		}
		sort.Strings(results)
		expected := []string{
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

	// Try to access a file.
	{
		ld := mounttable.GlobbableClient(naming.JoinAddressName(endpoint, "//foo.txt"))
		stream, err := ld.Glob(runtime.NewContext(), "*")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results := []string{}
		iterator := stream.RecvStream()
		for count := 0; iterator.Advance(); count++ {
			results = append(results, iterator.Value().Name)
		}
		sort.Strings(results)
		expected := []string{}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}

		if err := iterator.Err(); err != nil {
			t.Errorf("unexpected stream error: %v", iterator.Err())
		}
		if err := stream.Finish(); err != nil {
			t.Errorf("Finish failed: %v", err)
		}

		stream, err = ld.Glob(runtime.NewContext(), "")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results = []string{}
		iterator = stream.RecvStream()
		for count := 0; iterator.Advance(); count++ {
			results = append(results, iterator.Value().Name)
		}
		sort.Strings(results)
		expected = []string{""}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %#v, want %#v", results, expected)
		}

		if err := iterator.Err(); err != nil {
			t.Errorf("unexpected stream error: %v", iterator.Err())
		}
		if err := stream.Finish(); err != nil {
			t.Errorf("Finish failed: %v", err)
		}
	}
}
