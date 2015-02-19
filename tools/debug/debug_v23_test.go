package main_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"v.io/core/veyron/lib/testutil/v23tests"
)

//go:generate v23 test generate

func V23TestDebugGlob(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	inv := binary.Start("glob", "__debug/*")

	var want string
	for _, entry := range []string{"logs", "pprof", "stats", "vtrace"} {
		want += "__debug/" + entry + "\n"
	}
	if got := inv.Output(); got != want {
		i.Fatalf("unexpected output, want %s, got %s", want, got)
	}
}

func V23TestDebugGlobLogs(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// Create a temp file before we list the logs.
	fileName := filepath.Base(i.NewTempFile().Name())
	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	output := binary.Start("glob", "__debug/logs/*").Output()

	// The output should contain the filename.
	want := "/logs/" + fileName
	if !strings.Contains(output, want) {
		i.Fatalf("output should contain %s but did not\n%s", want, output)
	}
}

func V23TestReadHostname(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	path := "__debug/stats/system/hostname"
	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	got := binary.Start("stats", "read", path).Output()
	hostname, err := os.Hostname()
	if err != nil {
		i.Fatalf("Hostname() failed: %v", err)
	}
	if want := path + ": " + hostname + "\n"; got != want {
		i.Fatalf("unexpected output, want %q, got %q", want, got)
	}
}

func createTestLogFile(i *v23tests.T, content string) *os.File {
	file := i.NewTempFile()
	_, err := file.Write([]byte(content))
	if err != nil {
		i.Fatalf("Write failed: %v", err)
	}
	return file
}

func V23TestLogSize(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	testLogData := "This is a test log file"
	file := createTestLogFile(i, testLogData)

	// Check to ensure the file size is accurate
	str := strings.TrimSpace(binary.Start("logs", "size", "__debug/logs/"+filepath.Base(file.Name())).Output())
	got, err := strconv.Atoi(str)
	if err != nil {
		i.Fatalf("Atoi(\"%q\") failed", str)
	}
	want := len(testLogData)
	if got != want {
		i.Fatalf("unexpected output, want %d, got %d", got, want)
	}
}

func V23TestStatsRead(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	testLogData := "This is a test log file\n"
	file := createTestLogFile(i, testLogData)
	logName := filepath.Base(file.Name())
	runCount := 12
	for i := 0; i < runCount; i++ {
		binary.Start("logs", "read", "__debug/logs/"+logName).WaitOrDie(nil, nil)
	}

	got := binary.Start("stats", "read", "__debug/stats/ipc/server/routing-id/*/methods/ReadLog/latency-ms").Output()

	want := fmt.Sprintf("Count: %d", runCount)
	if !strings.Contains(got, want) {
		i.Fatalf("expected output to contain %s, but did not\n", want, got)
	}
}

func V23TestStatsWatch(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	testLogData := "This is a test log file\n"
	file := createTestLogFile(i, testLogData)
	logName := filepath.Base(file.Name())
	binary.Start("logs", "read", "__debug/logs/"+logName).WaitOrDie(nil, nil)

	inv := binary.Start("stats", "watch", "-raw", "__debug/stats/ipc/server/routing-id/*/methods/ReadLog/latency-ms")

	lineChan := make(chan string)
	// Go off and read the invocation's stdout.
	go func() {
		line, err := bufio.NewReader(inv.Stdout()).ReadString('\n')
		if err != nil {
			i.Fatalf("Could not read line from invocation")
		}
		lineChan <- line
	}()

	// Wait up to 10 seconds for some stats output. Either some output
	// occurs or the timeout expires without any output.
	select {
	case <-time.After(10 * time.Second):
		i.Errorf("Timed out waiting for output")
	case got := <-lineChan:
		// Expect one ReadLog call to have occurred.
		want := "latency-ms: {Count:1"
		if !strings.Contains(got, want) {
			i.Errorf("wanted but could not find %q in output\n%s", want, got)
		}
	}
}

func performTracedRead(debugBinary *v23tests.Binary, path string) string {
	return debugBinary.Start("--veyron.vtrace.sample_rate=1", "logs", "read", path).Output()
}

func V23TestVTrace(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	logContent := "Hello, world!\n"
	logPath := "__debug/logs/" + filepath.Base(createTestLogFile(i, logContent).Name())
	// Create a log file with tracing, read it and check that the resulting trace exists.
	got := performTracedRead(binary, logPath)
	if logContent != got {
		i.Fatalf("unexpected output: want %s, got %s", logContent, got)
	}

	// Grab the ID of the first and only trace.
	want, traceContent := 1, binary.Start("vtrace", "__debug/vtrace").Output()
	if count := strings.Count(traceContent, "Trace -"); count != want {
		i.Fatalf("unexpected trace count, want %d, got %d\n%s", want, count, traceContent)
	}
	fields := strings.Split(traceContent, " ")
	if len(fields) < 3 {
		i.Fatalf("expected at least 3 space-delimited fields, got %d\n", len(fields), traceContent)
	}
	traceId := fields[2]

	// Do a sanity check on the trace ID: it should be a 32-character hex ID prefixed with 0x
	if match, _ := regexp.MatchString("0x[0-9a-f]{32}", traceId); !match {
		i.Fatalf("wanted a 32-character hex ID prefixed with 0x, got %s", traceId)
	}

	// Do another traced read, this will generate a new trace entry.
	performTracedRead(binary, logPath)

	// Read vtrace, we should have 2 traces now.
	want, output := 2, binary.Start("vtrace", "__debug/vtrace").Output()
	if count := strings.Count(output, "Trace -"); count != want {
		i.Fatalf("unexpected trace count, want %d, got %d\n%s", want, count, output)
	}

	// Now ask for a particular trace. The output should contain exactly
	// one trace whose ID is equal to the one we asked for.
	want, got = 1, binary.Start("vtrace", "__debug/vtrace", traceId).Output()
	if count := strings.Count(got, "Trace -"); count != want {
		i.Fatalf("unexpected trace count, want %d, got %d\n%s", want, count, got)
	}
	fields = strings.Split(got, " ")
	if len(fields) < 3 {
		i.Fatalf("expected at least 3 space-delimited fields, got %d\n", len(fields), got)
	}
	got = fields[2]
	if traceId != got {
		i.Fatalf("unexpected traceId, want %s, got %s", traceId, got)
	}
}

func V23TestPprof(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	binary := i.BuildGoPkg("v.io/core/veyron/tools/debug")
	inv := binary.Start("pprof", "run", "__debug/pprof", "heap", "--text")

	// Assert that a profile indicating the heap size was written out.
	want, got := "(.*) of (.*) total", inv.Output()
	var groups []string
	if groups = regexp.MustCompile(want).FindStringSubmatch(got); len(groups) < 3 {
		i.Fatalf("could not find regexp %q in output\n%s", want, got)
	}
	i.Logf("got a heap profile showing a heap size of %s", groups[2])
}
