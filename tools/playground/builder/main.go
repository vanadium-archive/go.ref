// Compiles and runs code for the Veyron playground. Code is passed via os.Stdin
// as a JSON encoded request struct.

// NOTE(nlacasse): We use log.Panic() instead of log.Fatal() everywhere in this
// file.  We do this because log.Panic calls panic(), which allows any deferred
// function to run.  In particular, this will cause the mounttable and proxy
// processes to be killed in the event of a compilation error.  log.Fatal, on
// the other hand, calls os.Exit(1), which does not call deferred functions,
// and will leave proxy and mounttable processes running.  This is not a big
// deal for production environment, because the Docker instance gets cleaned up
// after each run, but during development and testing these extra processes can
// cause issues.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"veyron.io/veyron/veyron/tools/playground/event"
)

const runTimeout = 3 * time.Second

var (
	verbose = flag.Bool("v", false, "Whether to output debug messages")

	includeServiceOutput = flag.Bool("includeServiceOutput", false,
		"Whether to stream service (mounttable, wspr, proxy) output to clients")

	// Whether we have stopped execution of running files.
	stopped = false

	mu sync.Mutex
)

// Type of data sent to the builder on stdin.  Input should contain Files.  We
// look for a file whose Name ends with .id, and parse that into Identities.
//
// TODO(nlacasse): Update the identity parsing and usage to the new identity
// APIs.
//
// TODO(ribrdb): Consider moving identity parsing into the http server.
type request struct {
	Files      []*codeFile
	Identities []identity
}

// Type of file data.  Only Name and Body should be initially set.  The other
// fields are added as the file is parsed.
type codeFile struct {
	Name string
	Body string
	// Language the file is written in.  Inferred from the file extension.
	lang string
	// Identity to associate with the file's process.
	identity string
	// The executable flag denotes whether the file should be executed as
	// part of the playground run. This is currently used only for
	// javascript files, and go files with package "main".
	executable bool
	// Name of the binary (for go files).
	binaryName string
	// Running cmd process for the file.
	cmd *exec.Cmd
	// Any subprocesses that are needed to support running the file (e.g. wspr).
	subprocs []*os.Process
	// The index of the file in the request.
	index int
}

type exit struct {
	name string
	err  error
}

func debug(args ...interface{}) {
	if *verbose {
		log.Println(args...)
	}
}

func panicOnError(err error) {
	if err != nil {
		log.Panic(err)
	}
}

func parseRequest(in io.Reader) (r request, err error) {
	debug("Parsing input")
	data, err := ioutil.ReadAll(in)
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	m := make(map[string]*codeFile)
	for i := 0; i < len(r.Files); i++ {
		f := r.Files[i]
		f.index = i
		if path.Ext(f.Name) == ".id" {
			err = json.Unmarshal([]byte(f.Body), &r.Identities)
			if err != nil {
				return
			}
			r.Files = append(r.Files[:i], r.Files[i+1:]...)
			i--
		} else {
			switch path.Ext(f.Name) {
			case ".js":
				// JavaScript files are always executable.
				f.executable = true
				f.lang = "js"
			case ".go":
				// Go files will be marked as executable if their package name is
				// "main". This happens in the "maybeSetExecutableAndBinaryName"
				// function.
				f.lang = "go"
			case ".vdl":
				f.lang = "vdl"
			default:
				return r, fmt.Errorf("Unknown file type: %q", f.Name)
			}

			basename := path.Base(f.Name)
			if _, ok := m[basename]; ok {
				return r, fmt.Errorf("Two files with same basename: %q", basename)
			}
			m[basename] = f
		}
	}
	if len(r.Identities) == 0 {
		// Run everything with the same identity if none are specified.
		r.Identities = append(r.Identities, identity{Name: "default"})
		for _, f := range r.Files {
			f.identity = "default"
		}
	} else {
		for _, identity := range r.Identities {
			for _, basename := range identity.Files {
				// Check that the file associated with the identity exists.  We ignore
				// cases where it doesn't because the test .id files get used for
				// multiple different code files.  See testdata/ids/authorized.id, for
				// example.
				if m[basename] != nil {
					m[basename].identity = identity.Name
				}
			}
		}
	}
	return
}

func writeFiles(files []*codeFile) error {
	debug("Writing files")
	for _, f := range files {
		if err := f.write(); err != nil {
			return fmt.Errorf("Error writing %s: %v", f.Name, err)
		}
	}
	return nil
}

func compileFiles(files []*codeFile) error {
	needToCompile := false
	for _, f := range files {
		if f.lang == "vdl" || f.lang == "go" {
			needToCompile = true
			break
		}
	}
	if !needToCompile {
		return nil
	}

	debug("Compiling files")
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	os.Setenv("GOPATH", pwd+":"+os.Getenv("GOPATH"))
	os.Setenv("VDLPATH", pwd+":"+os.Getenv("VDLPATH"))
	// We set isService=false for compilation because "go install" only produces
	// output on error, and we always want clients to see such errors.
	return makeCmd("", false, "veyron", "go", "install", "./...").Run()
}

func runFiles(files []*codeFile) {
	debug("Running files")
	exit := make(chan exit)
	running := 0
	for _, f := range files {
		if f.executable {
			f.run(exit)
			running++
		}
	}

	timeout := time.After(runTimeout)

	for running > 0 {
		select {
		case <-timeout:
			panicOnError(writeEvent("", "stderr", "Playground exceeded deadline."))
			stopAll(files)
		case status := <-exit:
			if status.err == nil {
				panicOnError(writeEvent(status.name, "stdout", "Exited cleanly."))
			} else {
				panicOnError(writeEvent(status.name, "stderr", fmt.Sprintf("Exited with error: %v", status.err)))
			}
			running--
			stopAll(files)
		}
	}
}

func stopAll(files []*codeFile) {
	mu.Lock()
	defer mu.Unlock()
	if !stopped {
		stopped = true
		for _, f := range files {
			f.stop()
		}
	}
}

func (f *codeFile) maybeSetExecutableAndBinaryName() error {
	debug("Parsing package from", f.Name)
	file, err := parser.ParseFile(token.NewFileSet(), f.Name,
		strings.NewReader(f.Body), parser.PackageClauseOnly)
	if err != nil {
		return err
	}
	pkg := file.Name.String()
	if pkg == "main" {
		f.executable = true
		basename := path.Base(f.Name)
		f.binaryName = basename[:len(basename)-len(path.Ext(basename))]
	}
	return nil
}

func (f *codeFile) write() error {
	debug("Writing file", f.Name)
	if f.lang == "go" || f.lang == "vdl" {
		if err := f.maybeSetExecutableAndBinaryName(); err != nil {
			return err
		}
	}
	// Retain the original file tree structure.
	if err := os.MkdirAll(path.Dir(f.Name), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(f.Name, []byte(f.Body), 0644)
}

func (f *codeFile) startJs() error {
	wsprProc, wsprPort, err := startWspr(f.Name, f.identity)
	if err != nil {
		return fmt.Errorf("Error starting wspr: %v", err)
	}
	f.subprocs = append(f.subprocs, wsprProc)
	os.Setenv("WSPR", "http://localhost:"+strconv.Itoa(wsprPort))
	node := filepath.Join(os.Getenv("VEYRON_ROOT"), "environment", "cout", "node", "bin", "node")
	f.cmd = makeCmd(f.Name, false, node, f.Name)
	return f.cmd.Start()
}

func (f *codeFile) startGo() error {
	f.cmd = makeCmd(f.Name, false, filepath.Join("bin", f.binaryName))
	if f.identity != "" {
		f.cmd.Env = append(f.cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%s", filepath.Join("ids", f.identity)))
	}
	return f.cmd.Start()
}

func (f *codeFile) run(ch chan exit) {
	debug("Running", f.Name)
	err := func() error {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return fmt.Errorf("Execution has stopped; not running %s", f.Name)
		}

		switch f.lang {
		case "go":
			return f.startGo()
		case "js":
			return f.startJs()
		default:
			return fmt.Errorf("Cannot run file: %v", f.Name)
		}
	}()
	if err != nil {
		debug("Failed to start", f.Name)
		ch <- exit{f.Name, err}
		return
	}

	// Wait for the process to exit and send result to channel.
	go func() {
		debug("Waiting for", f.Name)
		err := f.cmd.Wait()
		debug("Done waiting for", f.Name)
		ch <- exit{f.Name, err}
	}()
}

func (f *codeFile) stop() {
	debug("Attempting to stop", f.Name)
	if f.cmd == nil {
		debug("Cannot stop:", f.Name, "cmd is nil")
	} else if f.cmd.Process == nil {
		debug("Cannot stop:", f.Name, "cmd is not nil, but cmd.Process is nil")
	} else {
		debug("Sending SIGTERM to", f.Name)
		f.cmd.Process.Signal(syscall.SIGTERM)
	}
	for i, subproc := range f.subprocs {
		debug("Killing subprocess", i, "for", f.Name)
		subproc.Kill()
	}
}

// Creates a cmd whose outputs (stdout and stderr) are streamed to stdout as
// json-encoded Event objects. If you want to watch the output streams yourself,
// add your own writer(s) to the multiWriter before starting the command.
func makeCmd(fileName string, isService bool, progName string, args ...string) *exec.Cmd {
	cmd := exec.Command(progName, args...)
	cmd.Env = os.Environ()
	stdout, stderr := newMultiWriter(), newMultiWriter()
	// TODO(sadovsky): Maybe annotate service output in the event stream.
	if !isService || *includeServiceOutput {
		stdout.Add(newEventStreamer(fileName, "stdout"))
		stderr.Add(newEventStreamer(fileName, "stderr"))
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd
}

// Initialize using newEventStreamer.
type eventStreamer struct {
	fileName   string
	streamName string
}

var _ io.Writer = (*eventStreamer)(nil)

func newEventStreamer(fileName, streamName string) *eventStreamer {
	return &eventStreamer{fileName: fileName, streamName: streamName}
}

func (es *eventStreamer) Write(p []byte) (n int, err error) {
	if err := writeEvent(es.fileName, es.streamName, string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func writeEvent(fileName, streamName, message string) error {
	e := event.Event{
		File:      fileName,
		Message:   message,
		Stream:    streamName,
		Timestamp: time.Now().UnixNano(),
	}
	jsonEvent, err := json.Marshal(e)
	if err != nil {
		return err
	}
	// TODO(nlacasse): When we switch over to actually streaming events, we'll
	// probably need to trigger a flush here.
	os.Stdout.Write(append(jsonEvent, '\n'))
	return nil
}

func main() {
	flag.Parse()
	r, err := parseRequest(os.Stdin)
	panicOnError(err)

	panicOnError(createIdentities(r.Identities))

	mt, err := startMount(runTimeout)
	panicOnError(err)
	defer mt.Kill()

	proxy, err := startProxy()
	panicOnError(err)
	defer proxy.Kill()

	panicOnError(writeFiles(r.Files))
	panicOnError(compileFiles(r.Files))
	runFiles(r.Files)
}
