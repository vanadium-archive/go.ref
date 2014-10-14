package debug_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/logreader"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/mounttable"
	"veyron.io/veyron/veyron2/verror"

	libstats "veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/services/mgmt/debug"
)

func TestDebugServer(t *testing.T) {
	runtime := rt.Init()

	workdir, err := ioutil.TempDir("", "logreadertest")
	if err != nil {
		t.Fatalf("ioutil.TempDir: %v", err)
	}
	defer os.RemoveAll(workdir)
	if err = ioutil.WriteFile(filepath.Join(workdir, "test.INFO"), []byte("test"), os.FileMode(0644)); err != nil {
		t.Fatalf("ioutil.WriteFile failed: %v", err)
	}

	endpoint, stop, err := debug.StartDebugServer(runtime, profiles.LocalListenSpec, workdir, nil)
	if err != nil {
		t.Fatalf("StartDebugServer failed: %v", err)
	}
	defer stop()

	// Access a logs directory that exists.
	{
		ld, err := mounttable.BindGlobbable(naming.JoinAddressName(endpoint, "//logs"))
		if err != nil {
			t.Errorf("BindGlobbable: %v", err)
		}
		stream, err := ld.Glob(runtime.NewContext(), "*")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results := []string{}
		iterator := stream.RecvStream()
		for count := 0; iterator.Advance(); count++ {
			results = append(results, iterator.Value().Name)
		}
		if len(results) != 1 || results[0] != "test.INFO" {
			t.Errorf("unexpected result. Got %v, want 'test.INFO'", results)
		}
		if err := iterator.Err(); err != nil {
			t.Errorf("unexpected stream error: %v", iterator.Err())
		}
		if err := stream.Finish(); err != nil {
			t.Errorf("Finish failed: %v", err)
		}
	}

	// Access a logs directory that doesn't exist.
	{
		ld, err := mounttable.BindGlobbable(naming.JoinAddressName(endpoint, "//logs/nowheretobefound"))
		if err != nil {
			t.Errorf("BindGlobbable: %v", err)
		}
		stream, err := ld.Glob(runtime.NewContext(), "*")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results := []string{}
		iterator := stream.RecvStream()
		for count := 0; iterator.Advance(); count++ {
			results = append(results, iterator.Value().Name)
		}
		if len(results) != 0 {
			t.Errorf("unexpected result. Got %v, want ''", results)
		}
		if expected, got := verror.NoExist, stream.Finish(); !verror.Is(got, expected) {
			t.Errorf("unexpected error value, got %v, want: %v", got, expected)
		}
	}

	// Access a log file that exists.
	{
		lf, err := logreader.BindLogFile(naming.JoinAddressName(endpoint, "//logs/test.INFO"))
		if err != nil {
			t.Errorf("BindLogFile: %v", err)
		}
		size, err := lf.Size(runtime.NewContext())
		if err != nil {
			t.Errorf("Size failed: %v", err)
		}
		if expected := int64(len("test")); size != expected {
			t.Errorf("unexpected result. Got %v, want %v", size, expected)
		}
	}

	// Access a log file that doesn't exist.
	{
		lf, err := logreader.BindLogFile(naming.JoinAddressName(endpoint, "//logs/nosuchfile.INFO"))
		if err != nil {
			t.Errorf("BindLogFile: %v", err)
		}
		_, err = lf.Size(runtime.NewContext())
		if expected := verror.NoExist; !verror.Is(err, expected) {
			t.Errorf("unexpected error value, got %v, want: %v", err, expected)
		}
	}

	// Access a stats object that exists.
	{
		foo := libstats.NewInteger("testing/foo")
		foo.Set(123)

		st, err := stats.BindStats(naming.JoinAddressName(endpoint, "//stats/testing/foo"))
		if err != nil {
			t.Errorf("BindStats: %v", err)
		}
		v, err := st.Value(runtime.NewContext())
		if err != nil {
			t.Errorf("Value failed: %v", err)
		}
		if expected := int64(123); v != expected {
			t.Errorf("unexpected result. Got %v, want %v", v, expected)
		}
	}

	// Access a stats object that doesn't exists.
	{
		st, err := stats.BindStats(naming.JoinAddressName(endpoint, "//stats/testing/nobodyhome"))
		if err != nil {
			t.Errorf("BindStats: %v", err)
		}
		_, err = st.Value(runtime.NewContext())
		if expected := verror.NoExist; !verror.Is(err, expected) {
			t.Errorf("unexpected error value, got %v, want: %v", err, expected)
		}
	}

	// Glob from the root.
	{
		ns := rt.R().Namespace()
		ns.SetRoots(naming.JoinAddressName(endpoint, "//"))
		ctx, cancel := rt.R().NewContext().WithTimeout(10 * time.Second)
		defer cancel()
		c, err := ns.Glob(ctx, "logs/...")
		if err != nil {
			t.Errorf("ns.Glob failed: %v", err)
		}
		results := []string{}
		for res := range c {
			results = append(results, res.Name)
		}
		sort.Strings(results)
		expected := []string{
			"logs",
			"logs/test.INFO",
		}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}

		c, err = ns.Glob(ctx, "stats/testing/*")
		if err != nil {
			t.Errorf("ns.Glob failed: %v", err)
		}
		results = []string{}
		for res := range c {
			results = append(results, res.Name)
		}
		sort.Strings(results)
		expected = []string{
			"stats/testing/foo",
		}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}

		c, err = ns.Glob(ctx, "*")
		if err != nil {
			t.Errorf("ns.Glob failed: %v", err)
		}
		results = []string{}
		for res := range c {
			results = append(results, res.Name)
		}
		sort.Strings(results)
		expected = []string{
			"logs",
			"pprof",
			"stats",
		}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}

		c, err = ns.Glob(ctx, "...")
		if err != nil {
			t.Errorf("ns.Glob failed: %v", err)
		}
		results = []string{}
		for res := range c {
			if strings.HasPrefix(res.Name, "stats/") && !strings.HasPrefix(res.Name, "stats/testing/") {
				// Skip any non-testing stats.
				continue
			}
			results = append(results, res.Name)
		}
		sort.Strings(results)
		expected = []string{
			"",
			"logs",
			"logs/test.INFO",
			"pprof",
			"stats",
			"stats/testing/foo",
		}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}
	}
}
