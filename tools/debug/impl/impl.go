package impl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"veyron.io/veyron/veyron/lib/cmdline"
	"veyron.io/veyron/veyron/lib/glob"
	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/services/mgmt/pprof/client"
	istats "veyron.io/veyron/veyron/services/mgmt/stats"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/logreader"
	logtypes "veyron.io/veyron/veyron2/services/mgmt/logreader/types"
	"veyron.io/veyron/veyron2/services/mgmt/pprof"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/watch"
	watchtypes "veyron.io/veyron/veyron2/services/watch/types"
	"veyron.io/veyron/veyron2/vom"
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
	vom.Register(istats.HistogramValue{})

	// logs read flags
	cmdRead.Flags.BoolVar(&follow, "f", false, "When true, read will wait for new log entries when it reaches the end of the file.")
	cmdRead.Flags.BoolVar(&verbose, "v", false, "When true, read will be more verbose.")
	cmdRead.Flags.IntVar(&numEntries, "n", int(logtypes.AllEntries), "The number of log entries to read.")
	cmdRead.Flags.Int64Var(&startPos, "o", 0, "The position, in bytes, from which to start reading the log file.")

	// stats value flags
	cmdValue.Flags.BoolVar(&raw, "raw", false, "When true, the command will display the raw value of the object.")
	cmdValue.Flags.BoolVar(&showType, "type", false, "When true, the type of the values will be displayed.")

	// stats watchglob flags
	cmdWatchGlob.Flags.BoolVar(&raw, "raw", false, "When true, the command will display the raw value of the object.")
	cmdWatchGlob.Flags.BoolVar(&showType, "type", false, "When true, the type of the values will be displayed.")

	// pprof flags
	cmdPProfRun.Flags.StringVar(&pprofCmd, "pprofcmd", "veyron go tool pprof", "The pprof command to use.")
}

var cmdGlob = &cmdline.Command{
	Run:      runGlob,
	Name:     "glob",
	Short:    "Returns all matching entries from the namespace",
	Long:     "Returns all matching entries from the namespace.",
	ArgsName: "<pattern> ...",
	ArgsLong: `
<pattern> is a glob pattern to match.
`,
}

func runGlob(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); got < want {
		return cmd.UsageErrorf("glob: incorrect number of arguments, got %d, want >=%d", got, want)
	}
	results := make(chan naming.MountEntry)
	errors := make(chan error)
	ctx := rt.R().NewContext()
	for _, a := range args {
		go doGlob(ctx, a, results, errors)
	}
	var lastErr error
	count := 0
L:
	for {
		select {
		case r := <-results:
			fmt.Fprint(cmd.Stdout(), r.Name)
			for _, s := range r.Servers {
				fmt.Fprintf(cmd.Stdout(), " %s (TTL %s)", s.Server, s.TTL)
			}
			fmt.Fprintln(cmd.Stdout())
		case e := <-errors:
			if e != nil {
				lastErr = e
				fmt.Fprintln(cmd.Stderr(), e)
			}
			count++
			if count == len(args) {
				break L
			}
		}
	}
	return lastErr
}

func doGlob(ctx context.T, pattern string, results chan<- naming.MountEntry, errors chan<- error) {
	ns := rt.R().Namespace()
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	c, err := ns.Glob(ctx, pattern)
	if err != nil {
		errors <- err
		return
	}
	for res := range c {
		results <- res
	}
	errors <- nil
}

var cmdRead = &cmdline.Command{
	Run:      runRead,
	Name:     "read",
	Short:    "Reads the content of a log file object.",
	Long:     "Reads the content of a log file object.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the log file object.
`,
}

func runRead(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); want != got {
		return cmd.UsageErrorf("read: incorrect number of arguments, got %d, want %d", got, want)
	}
	name := args[0]
	lf, err := logreader.BindLogFile(name)
	if err != nil {
		return err
	}
	stream, err := lf.ReadLog(rt.R().NewContext(), startPos, int32(numEntries), follow)
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

var cmdSize = &cmdline.Command{
	Run:      runSize,
	Name:     "size",
	Short:    "Returns the size of the a log file object",
	Long:     "Returns the size of the a log file object.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the log file object.
`,
}

func runSize(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); want != got {
		return cmd.UsageErrorf("size: incorrect number of arguments, got %d, want %d", got, want)
	}
	name := args[0]
	lf, err := logreader.BindLogFile(name)
	if err != nil {
		return err
	}
	size, err := lf.Size(rt.R().NewContext())
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), size)
	return nil
}

var cmdValue = &cmdline.Command{
	Run:      runValue,
	Name:     "value",
	Short:    "Returns the value of the a stats object",
	Long:     "Returns the value of the a stats object.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the stats object.
`,
}

func runValue(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); want != got {
		return cmd.UsageErrorf("value: incorrect number of arguments, got %d, want %d", got, want)
	}
	name := args[0]
	lf, err := stats.BindStats(name)
	if err != nil {
		return err
	}
	v, err := lf.Value(rt.R().NewContext())
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), formatValue(v))
	return nil
}

var cmdWatchGlob = &cmdline.Command{
	Run:      runWatchGlob,
	Name:     "watchglob",
	Short:    "Returns a stream of all matching entries and their values",
	Long:     "Returns a stream of all matching entries and their values",
	ArgsName: "<pattern> ...",
	ArgsLong: `
<pattern> is a glob pattern to match.
`,
}

func runWatchGlob(cmd *cmdline.Command, args []string) error {
	if want, got := 1, len(args); got < want {
		return cmd.UsageErrorf("watchglob: incorrect number of arguments, got %d, want >=%d", got, want)
	}

	results := make(chan string)
	errors := make(chan error)
	ctx := rt.R().NewContext()
	for _, a := range args {
		go doWatchGlob(ctx, a, results, errors)
	}
	var lastErr error
	count := 0
L:
	for {
		select {
		case r := <-results:
			fmt.Fprintln(cmd.Stdout(), r)
		case e := <-errors:
			if e != nil {
				lastErr = e
				fmt.Fprintln(cmd.Stderr(), e)
			}
			count++
			if count == len(args) {
				break L
			}
		}
	}
	return lastErr
}

func doWatchGlob(ctx context.T, pattern string, results chan<- string, errors chan<- error) {
	root, globPattern := naming.SplitAddressName(pattern)
	g, err := glob.Parse(globPattern)
	if err != nil {
		errors <- fmt.Errorf("glob.Parse(%s): %v", globPattern, err)
		return
	}
	var prefixElems []string
	prefixElems, g = g.SplitFixedPrefix()
	name := naming.Join(prefixElems...)
	if len(root) != 0 {
		name = naming.JoinAddressName(root, name)
	}
	c, err := watch.BindGlobWatcher(name)
	if err != nil {
		errors <- err
		return
	}
	stream, err := c.WatchGlob(ctx, watchtypes.GlobRequest{Pattern: g.String()})
	if err != nil {
		errors <- err
		return
	}
	iterator := stream.RecvStream()
	for iterator.Advance() {
		v := iterator.Value()
		results <- fmt.Sprintf("%s: %s", naming.Join(name, v.Name), formatValue(v.Value))
	}
	if err = iterator.Err(); err != nil {
		errors <- err
		return
	}
	if err = stream.Finish(); err != nil {
		errors <- err
	}
	errors <- nil
}

func formatValue(value interface{}) string {
	var buf bytes.Buffer
	if showType {
		fmt.Fprintf(&buf, "%T: ", value)
	}
	if raw {
		fmt.Fprintf(&buf, "%+v", value)
		return buf.String()
	}
	switch v := value.(type) {
	case istats.HistogramValue:
		writeASCIIHistogram(&buf, v)
	default:
		fmt.Fprintf(&buf, "%v", v)
	}
	return buf.String()
}

func writeASCIIHistogram(w io.Writer, h istats.HistogramValue) {
	scale := h.Count
	if scale > 100 {
		scale = 100
	}
	var avg float64
	if h.Count > 0 {
		avg = float64(h.Sum) / float64(h.Count)
	}
	fmt.Fprintf(w, "Count: %d Sum: %d Avg: %f\n", h.Count, h.Sum, avg)
	for i, b := range h.Buckets {
		var r string
		if i+1 < len(h.Buckets) {
			r = fmt.Sprintf("[%d,%d[", b.LowBound, h.Buckets[i+1].LowBound)
		} else {
			r = fmt.Sprintf("[%d,Inf", b.LowBound)
		}
		fmt.Fprintf(w, "%-18s: ", r)
		if b.Count > 0 && h.Count > 0 {
			fmt.Fprintf(w, "%s %d (%.1f%%)", strings.Repeat("*", int(b.Count*scale/h.Count)), b.Count, 100*float64(b.Count)/float64(h.Count))
		}
		if i+1 < len(h.Buckets) {
			fmt.Fprintln(w)
		}
	}
}

var cmdPProfRun = &cmdline.Command{
	Run:      runPProf,
	Name:     "run",
	Short:    "Runs the pprof tool",
	Long:     "Runs the pprof tool",
	ArgsName: "<name> <profile> [passthru args] ...",
	ArgsLong: `
<name> is the name of the pprof object.
<profile> the name of the profile to use.

All the [passthru args] are passed to the pprof tool directly, e.g.

$ debug pprof run a/b/c heap --text --lines
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
	listener, err := client.StartProxy(rt.R(), name)
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
	pp, err := pprof.BindPProf(name)
	if err != nil {
		return err
	}
	v, err := pp.Profiles(rt.R().NewContext())
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
	Short:    "Runs an http proxy to a pprof object",
	Long:     "Runs an http proxy to a pprof object",
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
	listener, err := client.StartProxy(rt.R(), name)
	if err != nil {
		return err
	}
	defer listener.Close()

	fmt.Fprintln(cmd.Stdout())
	fmt.Fprintf(cmd.Stdout(), "The pprof proxy is listening at http://%s/pprof\n", listener.Addr())
	fmt.Fprintln(cmd.Stdout())
	fmt.Fprintln(cmd.Stdout(), "Hit CTRL-C to exit")

	<-signals.ShutdownOnSignals()
	return nil
}

var cmdRoot = cmdline.Command{
	Name:  "debug",
	Short: "Command-line tool for interacting with the debug server",
	Long:  "Command-line tool for interacting with the debug server.",
	Children: []*cmdline.Command{
		cmdGlob,
		&cmdline.Command{
			Name:     "logs",
			Short:    "Accesses log files",
			Long:     "Accesses log files",
			Children: []*cmdline.Command{cmdRead, cmdSize},
		},
		&cmdline.Command{
			Name:     "stats",
			Short:    "Accesses stats",
			Long:     "Accesses stats",
			Children: []*cmdline.Command{cmdValue, cmdWatchGlob},
		},
		&cmdline.Command{
			Name:     "pprof",
			Short:    "Accesses profiling data",
			Long:     "Accesses profiling data",
			Children: []*cmdline.Command{cmdPProfRun, cmdPProfRunProxy},
		},
	},
}

func Root() *cmdline.Command {
	return &cmdRoot
}
