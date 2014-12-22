package testdata

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

func createTestLogFile(t *testing.T, env integration.TestEnvironment, content string) *os.File {
	file := env.TempFile()
	_, err := file.Write([]byte(content))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	return file
}

func TestLogSize(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	testLogData := "This is a test log file"
	file := createTestLogFile(t, env, testLogData)

	// Check to ensure the file size is accurate
	str := strings.TrimSpace(binary.Start("logs", "size", env.RootMT()+"/__debug/logs/"+filepath.Base(file.Name())).Output())
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
	testLogData := "This is a test log file\n"
	file := createTestLogFile(t, env, testLogData)
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

func performTracedRead(debugBinary integration.TestBinary, path string) string {
	return debugBinary.Start("--veyron.vtrace.sample_rate=1", "logs", "read", path).Output()
}

func TestVTrace(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()

	binary := env.BuildGoPkg("veyron.io/veyron/veyron/tools/debug")
	logContent := "Hello, world!\n"
	logPath := env.RootMT() + "/__debug/logs/" + filepath.Base(createTestLogFile(t, env, logContent).Name())
	// Create a log file with tracing, read it and check that the resulting trace exists.
	got := performTracedRead(binary, logPath)
	if logContent != got {
		t.Fatalf("unexpected output: want %s, got %s", logContent, got)
	}

	// Grab the ID of the first and only trace.
	want, traceContent := 1, binary.Start("vtrace", env.RootMT()+"/__debug/vtrace").Output()
	if count := strings.Count(traceContent, "Trace -"); count != want {
		t.Fatalf("unexpected trace count, want %d, got %d\n%s", want, count, traceContent)
	}
	fields := strings.Split(traceContent, " ")
	if len(fields) < 3 {
		t.Fatalf("expected at least 3 space-delimited fields, got %d\n", len(fields), traceContent)
	}
	traceId := fields[2]

	// Do a sanity check on the trace ID: it should be a 32-character hex ID.
	if match, _ := regexp.MatchString("[0-9a-f]{32}", traceId); !match {
		t.Fatalf("wanted a 32-character hex ID, got %s", traceId)
	}

	// Do another traced read, this will generate a new trace entry.
	performTracedRead(binary, logPath)

	// Read vtrace, we should have 2 traces now.
	want, output := 2, binary.Start("vtrace", env.RootMT()+"/__debug/vtrace").Output()
	if count := strings.Count(output, "Trace -"); count != want {
		t.Fatalf("unexpected trace count, want %d, got %d\n%s", want, count, output)
	}

	// Now ask for a particular trace. The output should contain exactly
	// one trace whose ID is equal to the one we asked for.
	want, got = 1, binary.Start("vtrace", env.RootMT()+"/__debug/vtrace", traceId).Output()
	if count := strings.Count(got, "Trace -"); count != want {
		t.Fatalf("unexpected trace count, want %d, got %d\n%s", want, count, got)
	}
	fields = strings.Split(got, " ")
	if len(fields) < 3 {
		t.Fatalf("expected at least 3 space-delimited fields, got %d\n", len(fields), got)
	}
	got = fields[2]
	if traceId != got {
		t.Fatalf("unexpected traceId, want %s, got %s", traceId, got)
	}
}
