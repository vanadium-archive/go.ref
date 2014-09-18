// Compiles and runs code for the Veyron playground.
// Code is passed via os.Stdin as a JSON encoded
// request struct.
package main

import (
	"bufio"
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

	"veyron/tools/playground/event"
)

const runTimeout = 3 * time.Second

var (
	verbose = flag.Bool("v", false, "Verbose mode")

	// Whether we have stopped execution of running files.
	stopped = false

	mu sync.Mutex
)

// Type of data sent to the builder on stdin.  Input should contain Files.  We
// look for a file whose Name ends with .id, and parse that into Identities.
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
	// Package name of the file (for go and vdl files).
	pkg string
	// Running cmd process for the file.
	cmd *exec.Cmd
	// Any subprocesses that are needed to support running the file (e.g. wspr).
	subProcs []*os.Process
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

func parseRequest(in io.Reader) (r request, err error) {
	debug("Parsing input")
	data, err := ioutil.ReadAll(in)
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	m := make(map[string]*codeFile)
	for i := 0; i < len(r.Files); {
		f := r.Files[i]
		f.index = i
		if path.Ext(f.Name) == ".id" {
			err = json.Unmarshal([]byte(f.Body), &r.Identities)
			if err != nil {
				return
			}
			r.Files = append(r.Files[:i], r.Files[i+1:]...)
		} else {
			switch path.Ext(f.Name) {
			case ".js":
				// Javascript files are always executable.
				f.executable = true
				f.lang = "js"
			case ".go":
				// Go files will be marked as executable if
				// their package name is "main". This happens
				// in the "readPackage" function.
				f.lang = "go"
			case ".vdl":
				f.lang = "vdl"
			default:
				return r, fmt.Errorf("Unknown file type %s", f.Name)
			}

			m[f.Name] = f
			i++
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
			for _, name := range identity.Files {
				// Check that the file associated with the
				// identity exists.  We ignore cases where it
				// doesn't because the test .id files get used
				// for multiple different code files.  See
				// testdata/ids/authorized.id, for example.
				if m[name] != nil {
					m[name].identity = identity.Name

				}
			}
		}
	}

	return
}

func main() {
	flag.Parse()
	r, err := parseRequest(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	err = createIdentities(r.Identities)
	if err != nil {
		log.Fatal(err)
	}

	mt, err := startMount(runTimeout)
	if err != nil {
		log.Fatal(err)
	}
	defer mt.Kill()

	proxy, err := startProxy()
	if err != nil {
		log.Fatal(err)
	}
	defer proxy.Kill()

	if err := writeFiles(r.Files); err != nil {
		log.Fatal(err)
	}

	if err := compileFiles(r.Files); err != nil {
		log.Fatal(err)
	}

	runFiles(r.Files)
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
	debug("Compiling files")
	var nonVdlFiles []*codeFile

	// Compile the vdl files first, since Go files may depend on *.vdl.go
	// generated files.
	for _, f := range files {
		if f.lang != "vdl" {
			nonVdlFiles = append(nonVdlFiles, f)
		} else {
			if err := f.compile(); err != nil {
				return fmt.Errorf("Error compiling %s: %v", f.Name, err)
			}
		}
	}

	// Compile the non-vdl files
	for _, f := range nonVdlFiles {
		if err := f.compile(); err != nil {
			return fmt.Errorf("Error compiling %s: %v", f.Name, err)
		}
	}
	return nil
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
			writeEvent("", "Playground exceeded deadline.", "stderr")
			stopAll(files)
		case status := <-exit:
			if status.err == nil {
				writeEvent(status.name, "Exited.", "stdout")
			} else {
				writeEvent(status.name, fmt.Sprintf("Error: %v", status.err), "stderr")
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

func (f *codeFile) readPackage() error {
	debug("Parsing package from ", f.Name)
	file, err := parser.ParseFile(token.NewFileSet(), f.Name,
		strings.NewReader(f.Body), parser.PackageClauseOnly)
	if err != nil {
		return err
	}
	f.pkg = file.Name.String()
	if "main" == f.pkg {
		f.executable = true
		basename := path.Base(f.Name)
		f.pkg = basename[:len(basename)-len(path.Ext(basename))]
	}
	return nil
}

func (f *codeFile) write() error {
	debug("Writing file ", f.Name)
	if f.lang == "go" || f.lang == "vdl" {
		if err := f.readPackage(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join("src", f.pkg), 0777); err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join("src", f.pkg, f.Name), []byte(f.Body), 0666)
}

func (f *codeFile) compile() error {
	debug("Compiling file ", f.Name)
	var cmd *exec.Cmd
	switch f.lang {
	case "js":
		return nil
	case "vdl":
		cmd = makeCmdJsonEvent(f.Name, "vdl", "generate", "--lang=go", f.pkg)
	case "go":
		cmd = makeCmdJsonEvent(f.Name, filepath.Join(os.Getenv("VEYRON_ROOT"), "scripts", "build", "go"), "install", f.pkg)
	default:
		return fmt.Errorf("Can't compile file %s with language %s.", f.Name, f.lang)
	}
	cmd.Stdout = cmd.Stderr
	err := cmd.Run()
	return err
}

func (f *codeFile) startJs() error {
	wsprProc, wsprPort, err := startWspr(f)
	if err != nil {
		return fmt.Errorf("Error starting wspr: %v", err)
	}
	f.subProcs = append(f.subProcs, wsprProc)
	os.Setenv("WSPR", "http://localhost:"+strconv.Itoa(wsprPort))
	node := filepath.Join(os.Getenv("VEYRON_ROOT"), "environment", "cout", "node", "bin", "node")
	cmd := makeCmdJsonEvent(f.Name, node, filepath.Join("src", f.Name))
	f.cmd = cmd
	return cmd.Start()
}

func (f *codeFile) startGo() error {
	cmd := makeCmdJsonEvent(f.Name, filepath.Join("bin", f.pkg))
	if f.identity != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%s", filepath.Join("ids", f.identity)))
	}
	f.cmd = cmd
	return cmd.Start()
}

func (f *codeFile) run(ch chan exit) {
	debug("Running", f.Name)
	err := func() error {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return fmt.Errorf("Execution already stopped, not running file %s", f.Name)
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
		debug("Error starting", f.Name)
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
	debug("Attempting to stop ", f.Name)
	if f.cmd == nil {
		debug("No cmd for", f.Name, "cannot stop.")
	} else if f.cmd.Process == nil {
		debug("f.cmd exists for", f.Name, "but f.cmd.Process is nil! Cannot stop!.")
	} else {
		debug("Sending SIGTERM to", f.Name)
		f.cmd.Process.Signal(syscall.SIGTERM)
	}

	for _, subProc := range f.subProcs {
		debug("Killing sub process for", f.Name)
		subProc.Kill()
	}
}

// Creates a cmd who's output (stdout and stderr) are streamed to stdout as
// json-encoded Event objects.
func makeCmdJsonEvent(fileName, prog string, args ...string) *exec.Cmd {
	cmd := exec.Command(prog, args...)
	cmd.Env = os.Environ()

	// TODO(nlacasse): There is a bug in this code which results in
	//   "read |0: bad file descriptor".
	// The error seems to be caused by our use of cmd.StdoutPipe/StderrPipe
	// and cmd.Wait.  In particular, we seem to be calling Wait after the
	// pipes have been closed.
	// See: http://stackoverflow.com/questions/20134095/why-do-i-get-bad-file-descriptor-in-this-go-program-using-stderr-and-ioutil-re
	// and https://code.google.com/p/go/issues/detail?id=2266
	//
	// One solution is to wrap cmd.Start/Run, so that wait is never called
	// before the pipes are closed.

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	go streamEvents(fileName, "stdout", stdout)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}
	go streamEvents(fileName, "stderr", stderr)
	return cmd
}

func streamEvents(fileName, stream string, in io.Reader) {
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		writeEvent(fileName, scanner.Text(), stream)
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func writeEvent(fileName, message, stream string) {
	e := event.Event{
		File:      fileName,
		Message:   message,
		Stream:    stream,
		Timestamp: time.Now().Unix(),
	}

	jsonEvent, err := json.Marshal(e)
	if err != nil {
		log.Fatal(err)
	}

	// TODO(nlacasse): when we switch to streaming, we'll probably need to
	// trigger a flush here.
	os.Stdout.Write(append(jsonEvent, '\n'))
}
