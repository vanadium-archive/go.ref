// Compiles and runs code for the Veyron playground.
// Code is passed via os.Stdin as a JSON encoded
// Request struct.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const RUN_TIMEOUT = time.Second

var (
	verbose = flag.Bool("v", false, "Verbose mode")

	// Whether we have stopped execution of running files.
	stopped = false

	mu sync.Mutex
)

type CodeFile struct {
	Name     string
	Body     string
	lang     string
	identity string
	pkg      string
	// The executable flag denotes whether the file should be executed as
	// part of the playground run. This is currently used only for
	// javascript files, and go files with package "main".
	executable bool
	cmd        *exec.Cmd
	subProcs   []*os.Process
	index      int
}

// The input on STDIN should only contain Files.  We look for a file
// whose Name ends with .id, and parse that into Identities.
//
// TODO(ribrdb): Consider moving identity parsing into the http server.
type Request struct {
	Files      []*CodeFile
	Identities []Identity
}

type Exit struct {
	name string
	err  error
}

func debug(args ...interface{}) {
	if *verbose {
		log.Println(args...)
	}
}

func parseRequest(in io.Reader) (r Request, err error) {
	debug("Parsing input")
	data, err := ioutil.ReadAll(in)
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	m := make(map[string]*CodeFile)
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
				f.lang = "js"
				f.executable = true
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
		r.Identities = append(r.Identities, Identity{Name: "default"})
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

	mt, err := startMount(RUN_TIMEOUT)
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

func writeFiles(files []*CodeFile) error {
	debug("Writing files")
	for _, f := range files {
		if err := f.write(); err != nil {
			return fmt.Errorf("Error writing %s: %v", f.Name, err)
		}
	}
	return nil
}

func compileFiles(files []*CodeFile) error {
	debug("Compiling files")
	var nonVdlFiles []*CodeFile

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

func runFiles(files []*CodeFile) {
	debug("Running files")
	exit := make(chan Exit)
	running := 0
	for _, f := range files {
		if f.executable {
			f.run(exit)
			running++
		}
	}

	timeout := time.After(RUN_TIMEOUT)

	for running > 0 {
		select {
		case <-timeout:
			log.Fatal("Process executed too long.")
		case status := <-exit:
			if status.err == nil {
				log.Printf("%s exited.", status.name)
			} else {
				log.Printf("%s exited with error %v", status.name, status.err)
			}
			running--
			stopAll(files)
		}
	}
}

func stopAll(files []*CodeFile) {
	mu.Lock()
	defer mu.Unlock()
	if !stopped {
		stopped = true
		for _, f := range files {
			f.stop()
		}
	}
}

func (f *CodeFile) readPackage() error {
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

func (f *CodeFile) write() error {
	debug("Writing file ", f.Name)
	if f.lang == "go" || f.lang == "vdl" {
		if err := f.readPackage(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(path.Join("src", f.pkg), 0777); err != nil {
		return err
	}
	return ioutil.WriteFile(path.Join("src", f.pkg, f.Name), []byte(f.Body), 0666)
}

func (f *CodeFile) compile() error {
	debug("Compiling file ", f.Name)
	var cmd *exec.Cmd
	switch f.lang {
	case "js":
		return nil
	case "vdl":
		cmd = makeCmd("vdl", "generate", "--lang=go", f.pkg)
	case "go":
		cmd = makeCmd(path.Join(os.Getenv("VEYRON_ROOT"), "veyron/scripts/build/go"), "install", f.pkg)
	default:
		return fmt.Errorf("Can't compile file %s with language %s.", f.Name, f.lang)
	}
	cmd.Stdout = cmd.Stderr
	err := cmd.Run()
	if err != nil {
		f.executable = false
	}
	return err
}

func (f *CodeFile) startJs() error {
	wsprProc, wsprPort, err := startWspr(f.index, f.identity)
	if err != nil {
		return fmt.Errorf("Error starting wspr: %v", err)
	}
	f.subProcs = append(f.subProcs, wsprProc)
	os.Setenv("WSPR", "http://localhost:"+strconv.Itoa(wsprPort))
	node := path.Join(os.Getenv("VEYRON_ROOT"), "/environment/cout/node/bin/node")
	cmd := makeCmd(node, path.Join("src", f.Name))
	f.cmd = cmd
	return cmd.Start()
}

func (f *CodeFile) startGo() error {
	cmd := makeCmd(path.Join("bin", f.pkg))
	if f.identity != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%s", path.Join("ids", f.identity)))
	}
	f.cmd = cmd
	return cmd.Start()
}

func (f *CodeFile) run(ch chan Exit) {
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
		ch <- Exit{f.Name, err}
		return
	}

	// Wait for the process to exit and send result to channel.
	go func() {
		debug("Waiting for", f.Name)
		err := f.cmd.Wait()
		debug("Done waiting for", f.Name)
		ch <- Exit{f.Name, err}
	}()
}

func (f *CodeFile) stop() {
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

func makeCmd(prog string, args ...string) *exec.Cmd {
	cmd := exec.Command(prog, args...)
	// TODO(ribrdb): prefix output with the name of the binary
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd
}
