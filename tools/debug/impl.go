package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"v.io/core/veyron/lib/glob"
	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/services/mgmt/pprof/client"
	istats "v.io/core/veyron/services/mgmt/stats"
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/logreader"
	logtypes "v.io/core/veyron2/services/mgmt/logreader/types"
	"v.io/core/veyron2/services/mgmt/pprof"
	"v.io/core/veyron2/services/mgmt/stats"
	vtracesvc "v.io/core/veyron2/services/mgmt/vtrace"
	"v.io/core/veyron2/services/watch"
	watchtypes "v.io/core/veyron2/services/watch/types"
	"v.io/core/veyron2/uniqueid"
	"v.io/core/veyron2/vdl/vdlutil"
	"v.io/core/veyron2/vtrace"
	"v.io/lib/cmdline"
)

var (
	follow     bool
	verbose    bool
	numEntries int
	startPos   int64
	raw        bool
	showType   bool
	pprofCmd   string
)

func init() {
	vdlutil.Register(istats.HistogramValue{})

	// logs read flags
	cmdLogsRead.Flags.BoolVar(&follow, "f", false, "When true, read will wait for new log entries when it reaches the end of the file.")
	cmdLogsRead.Flags.BoolVar(&verbose, "v", false, "When true, read will be more verbose.")
	cmdLogsRead.Flags.IntVar(&numEntries, "n", int(logtypes.AllEntries), "The number of log entries to read.")
	cmdLogsRead.Flags.Int64Var(&startPos, "o", 0, "The position, in bytes, from which to start reading the log file.")

	// stats read flags
	cmdStatsRead.Flags.BoolVar(&raw, "raw", false, "When true, the command will display the raw value of the object.")
	cmdStatsRead.Flags.BoolVar(&showType, "type", false, "When true, the type of the values will be displayed.")

	// stats watch flags
	cmdStatsWatch.Flags.BoolVar(&raw, "raw", false, "When true, the command will display the raw value of the object.")
	cmdStatsWatch.Flags.BoolVar(&showType, "type", false, "When true, the type of the values will be displayed.")

	// pprof flags
	cmdPProfRun.Flags.StringVar(&pprofCmd, "pprofcmd", "v23 go tool pprof", "The pprof command to use.")
}

var cmdVtrace = &cmdline.Command{
	Run:      runVtrace,
	Name:     "vtrace",
	Short:    "Returns vtrace traces.",
	Long:     "Returns matching vtrace traces (or all stored traces if no ids are given).",
	ArgsName: "<name> [id ...]",
	ArgsLong: `
<name> is the name of a vtrace object.
[id] is a vtrace trace id.
`,
}

func doFetchTrace(ctx *context.T, wg *sync.WaitGroup, client vtracesvc.StoreClientStub,
	id uniqueid.ID, traces chan *vtrace.TraceRecord, errors chan error) {
	defer wg.Done()

	trace, err := client.Trace(ctx, id)
	if err != nil {
		errors <- err
	} else {
		traces <- &trace
	}
}

func runVtrace(cmd *cmdline.Command, args []string) error {
	ctx := runtime.NewContext()
	arglen := len(args)
	if arglen == 0 {
		return cmd.UsageErrorf("vtrace: incorrect number of arguments, got %d want >= 1", arglen)
	}

	name := args[0]
	client := vtracesvc.StoreClient(name)
	if arglen == 1 {
		call, err := client.AllTraces(ctx)
		if err != nil {
			return err
		}
		stream := call.RecvStream()
		for stream.Advance() {
			trace := stream.Value()
			vtrace.FormatTrace(os.Stdout, &trace, nil)
		}
		if err := stream.Err(); err != nil {
			return err
		}
		return call.Finish()
	}

	ntraces := len(args) - 1
	traces := make(chan *vtrace.TraceRecord, ntraces)
	errors := make(chan error, ntraces)
	var wg sync.WaitGroup
	wg.Add(ntraces)
	for _, arg := range args[1:] {
		id, err := uniqueid.FromHexString(arg)
		if err != nil {
			return err
		}
		go doFetchTrace(ctx, &wg, client, id, traces, errors)
	}
	go func() {
		wg.Wait()
		close(traces)
		close(errors)
	}()

	for trace := range traces {
		vtrace.FormatTrace(os.Stdout, trace, nil)
	}

	// Just return one of the errors.
	return <-errors
}

var cmdGlob = &cmdline.Command{
	Run:      runGlob,
	Name:     "glob",
	Short:    "Returns all matching entries from the namespace.",
	Long:     "Returns all matching entries from the namespace.",
	ArgsName: "<pattern> ...",
	ArgsLong: `
<pattern> is a glob pattern to match.
`,
}

func runGlob(cmd *cmdline.Command, args []string) error {
	if min, got := 1, len(args); got < min {
		return cmd.UsageErrorf("glob: incorrect number of arguments, got %d, want >=%d", got, min)
	}
	results := make(chan naming.MountEntry)
	errors := make(chan error)
	doGlobs(runtime.NewContext(), args, results, errors)
	var lastErr error
	for {
		select {
		case err := <-errors:
			lastErr = err
			fmt.Fprintln(cmd.Stderr(), "Error:", err)
		case me, ok := <-results:
			if !ok {
				return lastErr
			}
			fmt.Fprint(cmd.Stdout(), me.Name)
			for _, s := range me.Servers {
				fmt.Fprintf(cmd.Stdout(), " %s (Expires %s)", s.Server, s.Expires)
			}
			fmt.Fprintln(cmd.Stdout())
		}
	}
}

// doGlobs calls Glob on multiple patterns in parallel and sends all the results
// on the results channel and all the errors on the errors channel. It closes
// the results channel when all the results have been sent.
func doGlobs(ctx *context.T, patterns []string, results chan<- naming.MountEntry, errors chan<- error) {
	var wg sync.WaitGroup
	wg.Add(len(patterns))
	for _, p := range patterns {
		go doGlob(ctx, p, results, errors, &wg)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
}

func doGlob(ctx *context.T, pattern string, results chan<- naming.MountEntry, errors chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	c, err := veyron2.GetNamespace(ctx).Glob(ctx, pattern)
	if err != nil {
		errors <- fmt.Errorf("%s: %v", pattern, err)
		return
	}
	for me := range c {
		results <- me
	}
}

var cmdLogsRead = &cmdline.Command{
	Run:      runLogsRead,
	Name:     "read",
	Short:    "Reads the content of a log file object.",
	Long:     "Reads the content of a log file object.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the log file object.
`,
}

func runLogsRead(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); want != got {
		return cmd.UsageErrorf("read: incorrect number of arguments, got %d, want %d", got, want)
	}
	name := args[0]
	lf := logreader.LogFileClient(name)
	stream, err := lf.ReadLog(runtime.NewContext(), startPos, int32(numEntries), follow)
	if err != nil {
		return err
	}
	iterator := stream.RecvStream()
	for iterator.Advance() {
		entry := iterator.Value()
		if verbose {
			fmt.Fprintf(cmd.Stdout(), "[%d] %s\n", entry.Position, entry.Line)
		} else {
			fmt.Fprintf(cmd.Stdout(), "%s\n", entry.Line)
		}
	}
	if err = iterator.Err(); err != nil {
		return err
	}
	offset, err := stream.Finish()
	if err != nil {
		return err
	}
	if verbose {
		fmt.Fprintf(cmd.Stdout(), "Offset: %d\n", offset)
	}
	return nil
}

var cmdLogsSize = &cmdline.Command{
	Run:      runLogsSize,
	Name:     "size",
	Short:    "Returns the size of a log file object.",
	Long:     "Returns the size of a log file object.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the log file object.
`,
}

func runLogsSize(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); want != got {
		return cmd.UsageErrorf("size: incorrect number of arguments, got %d, want %d", got, want)
	}
	name := args[0]
	lf := logreader.LogFileClient(name)
	size, err := lf.Size(runtime.NewContext())
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), size)
	return nil
}

var cmdStatsRead = &cmdline.Command{
	Run:      runStatsRead,
	Name:     "read",
	Short:    "Returns the value of stats objects.",
	Long:     "Returns the value of stats objects.",
	ArgsName: "<name> ...",
	ArgsLong: `
<name> is the name of a stats object, or a glob pattern to match against stats
object names.
`,
}

func runStatsRead(cmd *cmdline.Command, args []string) error {
	if min, got := 1, len(args); got < min {
		return cmd.UsageErrorf("read: incorrect number of arguments, got %d, want >=%d", got, min)
	}
	ctx := runtime.NewContext()
	globResults := make(chan naming.MountEntry)
	errors := make(chan error)
	doGlobs(ctx, args, globResults, errors)

	output := make(chan string)
	go func() {
		var wg sync.WaitGroup
		for me := range globResults {
			wg.Add(1)
			go doValue(ctx, me.Name, output, errors, &wg)
		}
		wg.Wait()
		close(output)
	}()

	var lastErr error
	for {
		select {
		case err := <-errors:
			lastErr = err
			fmt.Fprintln(cmd.Stderr(), err)
		case out, ok := <-output:
			if !ok {
				return lastErr
			}
			fmt.Fprintln(cmd.Stdout(), out)
		}
	}
}

func doValue(ctx *context.T, name string, output chan<- string, errors chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	v, err := stats.StatsClient(name).Value(ctx)
	if err != nil {
		errors <- fmt.Errorf("%s: %v", name, err)
		return
	}
	output <- fmt.Sprintf("%s: %v", name, formatValue(v))
}

var cmdStatsWatch = &cmdline.Command{
	Run:      runStatsWatch,
	Name:     "watch",
	Short:    "Returns a stream of all matching entries and their values as they change.",
	Long:     "Returns a stream of all matching entries and their values as they change.",
	ArgsName: "<pattern> ...",
	ArgsLong: `
<pattern> is a glob pattern to match.
`,
}

func runStatsWatch(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); got < want {
		return cmd.UsageErrorf("watch: incorrect number of arguments, got %d, want >=%d", got, want)
	}

	results := make(chan string)
	errors := make(chan error)
	ctx := runtime.NewContext()
	var wg sync.WaitGroup
	wg.Add(len(args))
	for _, arg := range args {
		go doWatch(ctx, arg, results, errors, &wg)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	var lastErr error
	for {
		select {
		case err := <-errors:
			lastErr = err
			fmt.Fprintln(cmd.Stderr(), "Error:", err)
		case r, ok := <-results:
			if !ok {
				return lastErr
			}
			fmt.Fprintln(cmd.Stdout(), r)
		}
	}
}

func doWatch(ctx *context.T, pattern string, results chan<- string, errors chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	root, globPattern := naming.SplitAddressName(pattern)
	g, err := glob.Parse(globPattern)
	if err != nil {
		errors <- fmt.Errorf("%s: %v", globPattern, err)
		return
	}
	var prefixElems []string
	prefixElems, g = g.SplitFixedPrefix()
	name := naming.Join(prefixElems...)
	if len(root) != 0 {
		name = naming.JoinAddressName(root, name)
	}
	c := watch.GlobWatcherClient(name)
	for retry := false; ; retry = true {
		if retry {
			time.Sleep(10 * time.Second)
		}
		stream, err := c.WatchGlob(ctx, watchtypes.GlobRequest{Pattern: g.String()})
		if err != nil {
			errors <- fmt.Errorf("%s: %v", name, err)
			continue
		}
		iterator := stream.RecvStream()
		for iterator.Advance() {
			v := iterator.Value()
			results <- fmt.Sprintf("%s: %s", naming.Join(name, v.Name), formatValue(v.Value))
		}
		if err = iterator.Err(); err != nil {
			errors <- fmt.Errorf("%s: %v", name, err)
			continue
		}
		if err = stream.Finish(); err != nil {
			errors <- fmt.Errorf("%s: %v", name, err)
		}
	}
}

func formatValue(value interface{}) string {
	var buf bytes.Buffer
	if showType {
		fmt.Fprintf(&buf, "%T: ", value)
	}
	if raw {
		if v, ok := value.(istats.HistogramValue); ok {
			// Bypass HistogramValue.String()
			type hist istats.HistogramValue
			value = hist(v)
		}
		fmt.Fprintf(&buf, "%+v", value)
		return buf.String()
	}
	fmt.Fprintf(&buf, "%v", value)
	return buf.String()
}

var cmdPProfRun = &cmdline.Command{
	Run:      runPProf,
	Name:     "run",
	Short:    "Runs the pprof tool.",
	Long:     "Runs the pprof tool.",
	ArgsName: "<name> <profile> [passthru args] ...",
	ArgsLong: `
<name> is the name of the pprof object.
<profile> the name of the profile to use.

All the [passthru args] are passed to the pprof tool directly, e.g.

$ debug pprof run a/b/c heap --text
$ debug pprof run a/b/c profile -gv
`,
}

func runPProf(cmd *cmdline.Command, args []string) error {
	if min, got := 1, len(args); got < min {
		return cmd.UsageErrorf("pprof: incorrect number of arguments, got %d, want >=%d", got, min)
	}
	name := args[0]
	if len(args) == 1 {
		return showPProfProfiles(cmd, name)
	}
	profile := args[1]
	listener, err := client.StartProxy(runtime, name)
	if err != nil {
		return err
	}
	defer listener.Close()

	// Construct the pprof command line:
	// <pprofCmd> http://<proxyaddr>/pprof/<profile> [pprof flags]
	pargs := []string{pprofCmd} // pprofCmd is purposely not escaped.
	for i := 2; i < len(args); i++ {
		pargs = append(pargs, shellEscape(args[i]))
	}
	pargs = append(pargs, shellEscape(fmt.Sprintf("http://%s/pprof/%s", listener.Addr(), profile)))
	pcmd := strings.Join(pargs, " ")
	fmt.Fprintf(cmd.Stdout(), "Running: %s\n", pcmd)
	c := exec.Command("sh", "-c", pcmd)
	c.Stdin = os.Stdin
	c.Stdout = cmd.Stdout()
	c.Stderr = cmd.Stderr()
	return c.Run()
}

func showPProfProfiles(cmd *cmdline.Command, name string) error {
	v, err := pprof.PProfClient(name).Profiles(runtime.NewContext())
	if err != nil {
		return err
	}
	v = append(v, "profile")
	sort.Strings(v)
	fmt.Fprintln(cmd.Stdout(), "Available profiles:")
	for _, p := range v {
		fmt.Fprintf(cmd.Stdout(), "  %s\n", p)
	}
	return nil
}

func shellEscape(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	re := regexp.MustCompile("([\"$`\\\\])")
	return `"` + re.ReplaceAllString(s, "\\$1") + `"`
}

var cmdPProfRunProxy = &cmdline.Command{
	Run:      runPProfProxy,
	Name:     "proxy",
	Short:    "Runs an http proxy to a pprof object.",
	Long:     "Runs an http proxy to a pprof object.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the pprof object.
`,
}

func runPProfProxy(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); got != want {
		return cmd.UsageErrorf("proxy: incorrect number of arguments, got %d, want %d", got, want)
	}
	name := args[0]
	listener, err := client.StartProxy(runtime, name)
	if err != nil {
		return err
	}
	defer listener.Close()

	fmt.Fprintln(cmd.Stdout())
	fmt.Fprintf(cmd.Stdout(), "The pprof proxy is listening at http://%s/pprof\n", listener.Addr())
	fmt.Fprintln(cmd.Stdout())
	fmt.Fprintln(cmd.Stdout(), "Hit CTRL-C to exit")

	ctx := runtime.NewContext()

	<-signals.ShutdownOnSignals(ctx)
	return nil
}

var cmdRoot = cmdline.Command{
	Name:  "debug",
	Short: "Command-line tool for interacting with the debug server.",
	Long:  "Command-line tool for interacting with the debug server.",
	Children: []*cmdline.Command{
		cmdGlob,
		cmdVtrace,
		&cmdline.Command{
			Name:     "logs",
			Short:    "Accesses log files",
			Long:     "Accesses log files",
			Children: []*cmdline.Command{cmdLogsRead, cmdLogsSize},
		},
		&cmdline.Command{
			Name:     "stats",
			Short:    "Accesses stats",
			Long:     "Accesses stats",
			Children: []*cmdline.Command{cmdStatsRead, cmdStatsWatch},
		},
		&cmdline.Command{
			Name:     "pprof",
			Short:    "Accesses profiling data",
			Long:     "Accesses profiling data",
			Children: []*cmdline.Command{cmdPProfRun, cmdPProfRunProxy},
		},
	},
}

func root() *cmdline.Command {
	return &cmdRoot
}
