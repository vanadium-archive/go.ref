package debug

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/context"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/services/mgmt/logreader"
	"v.io/veyron/veyron2/services/mgmt/stats"
	vtracesvc "v.io/veyron/veyron2/services/mgmt/vtrace"
	verror "v.io/veyron/veyron2/verror2"
	"v.io/veyron/veyron2/vtrace"

	libstats "v.io/veyron/veyron/lib/stats"
	"v.io/veyron/veyron/lib/testutil"
	"v.io/veyron/veyron/profiles"
)

// startDebugServer starts a debug server.
func startDebugServer(rt veyron2.Runtime, listenSpec ipc.ListenSpec, logsDir string) (string, func(), error) {
	if len(logsDir) == 0 {
		return "", nil, fmt.Errorf("logs directory missing")
	}
	disp := NewDispatcher(logsDir, nil, rt.VtraceStore())
	server, err := rt.NewServer()
	if err != nil {
		return "", nil, fmt.Errorf("failed to start debug server: %v", err)
	}
	endpoints, err := server.Listen(listenSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen on %s: %v", listenSpec, err)
	}
	if err := server.ServeDispatcher("", disp); err != nil {
		return "", nil, err
	}
	ep := endpoints[0].String()
	return ep, func() { server.Stop() }, nil
}

func TestDebugServer(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()

	tracedContext := func() context.T {
		ctx := runtime.NewContext()
		vtrace.FromContext(ctx).Trace().ForceCollect()
		return ctx
	}
	rootName = "debug"

	workdir, err := ioutil.TempDir("", "logreadertest")
	if err != nil {
		t.Fatalf("ioutil.TempDir: %v", err)
	}
	defer os.RemoveAll(workdir)
	if err = ioutil.WriteFile(filepath.Join(workdir, "test.INFO"), []byte("test"), os.FileMode(0644)); err != nil {
		t.Fatalf("ioutil.WriteFile failed: %v", err)
	}

	endpoint, stop, err := startDebugServer(runtime, profiles.LocalListenSpec, workdir)
	if err != nil {
		t.Fatalf("StartDebugServer failed: %v", err)
	}
	defer stop()

	// Access a logs directory that exists.
	{
		results, err := testutil.GlobName(runtime.NewContext(), naming.JoinAddressName(endpoint, "debug/logs"), "*")
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		if len(results) != 1 || results[0] != "test.INFO" {
			t.Errorf("unexpected result. Got %v, want 'test.INFO'", results)
		}
	}

	// Access a logs directory that doesn't exist.
	{
		results, err := testutil.GlobName(runtime.NewContext(), naming.JoinAddressName(endpoint, "debug/logs/nowheretobefound"), "*")
		if len(results) != 0 {
			t.Errorf("unexpected result. Got %v, want ''", results)
		}
		if err != nil {
			t.Errorf("unexpected error value: %v", err)
		}
	}

	// Access a log file that exists.
	{
		lf := logreader.LogFileClient(naming.JoinAddressName(endpoint, "debug/logs/test.INFO"))
		size, err := lf.Size(tracedContext())
		if err != nil {
			t.Errorf("Size failed: %v", err)
		}
		if expected := int64(len("test")); size != expected {
			t.Errorf("unexpected result. Got %v, want %v", size, expected)
		}
	}

	// Access a log file that doesn't exist.
	{
		lf := logreader.LogFileClient(naming.JoinAddressName(endpoint, "debug/logs/nosuchfile.INFO"))
		_, err = lf.Size(tracedContext())
		if expected := verror.NoExist.ID; !verror.Is(err, expected) {
			t.Errorf("unexpected error value, got %v, want: %v", err, expected)
		}
	}

	// Access a stats object that exists.
	{
		foo := libstats.NewInteger("testing/foo")
		foo.Set(123)

		st := stats.StatsClient(naming.JoinAddressName(endpoint, "debug/stats/testing/foo"))
		v, err := st.Value(tracedContext())
		if err != nil {
			t.Errorf("Value failed: %v", err)
		}
		if expected := int64(123); v != expected {
			t.Errorf("unexpected result. Got %v, want %v", v, expected)
		}
	}

	// Access a stats object that doesn't exists.
	{
		st := stats.StatsClient(naming.JoinAddressName(endpoint, "debug/stats/testing/nobodyhome"))
		_, err = st.Value(tracedContext())
		if expected := verror.NoExist.ID; !verror.Is(err, expected) {
			t.Errorf("unexpected error value, got %v, want: %v", err, expected)
		}
	}

	// Access vtrace.
	{
		vt := vtracesvc.StoreClient(naming.JoinAddressName(endpoint, "debug/vtrace"))
		call, err := vt.AllTraces(runtime.NewContext())
		if err != nil {
			t.Errorf("AllTraces failed: %v", err)
		}
		ntraces := 0
		stream := call.RecvStream()
		for stream.Advance() {
			stream.Value()
			ntraces++
		}
		if err = stream.Err(); err != nil && err != io.EOF {
			t.Fatalf("Unexpected error reading trace stream: %s", err)
		}
		if ntraces != 4 {
			t.Errorf("We expected 4 traces, got: %d", ntraces)
		}
	}

	// Glob from the root.
	{
		ns := runtime.Namespace()
		ns.SetRoots(naming.JoinAddressName(endpoint, "debug"))
		ctx, cancel := runtime.NewContext().WithTimeout(10 * time.Second)
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
			"vtrace",
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
			"vtrace",
		}
		if !reflect.DeepEqual(expected, results) {
			t.Errorf("unexpected result. Got %v, want %v", results, expected)
		}
	}
}
