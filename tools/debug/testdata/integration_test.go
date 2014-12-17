package testdata

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil/integration"

	_ "veyron.io/veyron/veyron/profiles/static"
)

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestDebugGlob(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	inv := binary.Start("glob", env.RootMT()+"/__debug/*")

	var want string
	for _, entry := range []string{"logs", "pprof", "stats", "vtrace"} {
		want += env.RootMT() + "/__debug/" + entry + "\n"
	}
	if got := inv.Output(); got != want {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}
}

func TestDebugGlobLogs(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	fileName := filepath.Base(env.TempFile().Name())
	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	output := binary.Start("glob", env.RootMT()+"/__debug/logs/*").Output()

	// The output should contain the filename.
	want := "/logs/" + fileName
	if !strings.Contains(output, want) {
		t.Fatalf("output should contain %s but did not\n%s", want, output)
	}
}

func TestReadHostname(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	path := env.RootMT() + "/__debug/stats/system/hostname"
	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	got := binary.Start("stats", "read", path).Output()
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname() failed: %v", err)
	}
	if want := path + ": " + hostname + "\n"; got != want {
		t.Fatalf("unexpected output, want %s, got %s", want, got)
	}
}

func TestLogSize(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	f := env.TempFile()
	testLogData := []byte("This is a test log file")
	f.Write(testLogData)

	// Check to ensure the file size is accurate
	str := strings.TrimSpace(binary.Start("logs", "size", env.RootMT()+"/__debug/logs/"+filepath.Base(f.Name())).Output())
	got, err := strconv.Atoi(str)
	if err != nil {
		t.Fatalf("Atoi(\"%q\") failed", str)
	}
	want := len(testLogData)
	if got != want {
		t.Fatalf("unexpected output, want %d, got %d", got, want)
	}
}

func TestStatsRead(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	file := env.TempFile()
	testLogData := []byte("This is a test log file\n")
	file.Write(testLogData)
	logName := filepath.Base(file.Name())
	runCount := 12
	for i := 0; i < runCount; i++ {
		binary.Start("logs", "read", env.RootMT()+"/__debug/logs/"+logName).Wait(nil, nil)
	}
	got := binary.Start("stats", "read", env.RootMT()+"/__debug/stats/ipc/server/routing-id/*/methods/ReadLog/latency-ms").Output()
	want := fmt.Sprintf("Count: %d", runCount)
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %s, but did not\n", want, got)
	}
}
