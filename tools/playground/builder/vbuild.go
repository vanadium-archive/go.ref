// Compiles and runs code for the Veyron playground.
// Code is passed via os.Stdin as a JSON encoded
// Request struct.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const RUN_TIMEOUT = time.Second
const debug = false

type CodeFile struct {
	Name       string
	Body       string
	identity   string
	pkg        string
	executable bool
	proc       *exec.Cmd
}

type Identity struct {
	Name     string
	Blesser  string
	Duration string
	Files    []string
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

func Log(args ...interface{}) {
	if debug {
		log.Println(args...)
	}
}

func MakeCmd(prog string, args ...string) *exec.Cmd {
	Log("Running", prog, strings.Join(args, " "))
	cmd := exec.Command(prog, args...)
	// TODO(ribrdb): prefix output with the name of the binary
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd
}

func ParseRequest(in io.Reader) (r Request, err error) {
	Log("Parsing input")
	data, err := ioutil.ReadAll(in)
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	m := make(map[string]*CodeFile)
	for i := 0; i < len(r.Files); {
		f := r.Files[i]
		if path.Ext(f.Name) == ".id" {
			err = json.Unmarshal([]byte(f.Body), &r.Identities)
			if err != nil {
				return
			}
			r.Files = append(r.Files[:i], r.Files[i+1:]...)
		} else {
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
				m[name].identity = identity.Name
			}
		}
	}

	return
}

func main() {
	r, err := ParseRequest(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	CreateIdentities(r)
	if err = StartMount(); err != nil {
		log.Fatal(err)
	}
	CompileAndRun(r)
}

func CreateIdentities(r Request) error {
	Log("Generating identities")
	if err := os.MkdirAll("ids", 0777); err != nil {
		return err
	}
	for _, id := range r.Identities {
		if err := id.Create(); err != nil {
			return err
		}
	}
	return nil
}

func CompileAndRun(r Request) {
	Log("Processing files")
	exit := make(chan Exit)
	running := 0

	// TODO(ribrdb): Compile first, don't run anything if compilation fails.

	for _, f := range r.Files {
		var err error
		if err = f.Write(); err != nil {
			goto Error
		}
		if err = f.Compile(); err != nil {
			goto Error
		}
		if f.executable {
			go f.Run(exit)
			running++
		}
	Error:
		if err != nil {
			log.Printf("%s: %v\n", f.Name, err)
		}
	}

	timeout := time.After(RUN_TIMEOUT)
	killed := false

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
			if !killed {
				killed = true
				for _, f := range r.Files {
					if f.Name != status.name && f.proc != nil {
						f.proc.Process.Signal(syscall.SIGTERM)
					}
				}
			}
		}
	}
}

func (file *CodeFile) readPackage() error {
	Log("Parsing package from ", file.Name)
	f, err := parser.ParseFile(token.NewFileSet(), file.Name,
		strings.NewReader(file.Body), parser.PackageClauseOnly)
	if err != nil {
		return err
	}
	file.pkg = f.Name.String()
	if "main" == file.pkg {
		file.executable = true
		basename := path.Base(file.Name)
		file.pkg = basename[:len(basename)-len(path.Ext(basename))]
	}
	return nil
}

func (f *CodeFile) Write() error {
	if err := f.readPackage(); err != nil {
		return err
	}
	if err := os.MkdirAll(path.Join("src", f.pkg), 0777); err != nil {
		return err
	}
	Log("Writing file ", f.Name)
	return ioutil.WriteFile(path.Join("src", f.pkg, f.Name), []byte(f.Body), 0666)
}

func (f *CodeFile) Compile() error {
	var cmd *exec.Cmd
	if path.Ext(f.Name) == ".idl" {
		cmd = MakeCmd("idlc", "generate", "-status=false", f.pkg)
	} else {
		cmd = MakeCmd("go", "install", f.pkg)
		cmd.Dir = "/usr/local/veyron/veyron"
	}
	cmd.Stdout = cmd.Stderr
	err := cmd.Run()
	if err != nil {
		f.executable = false
	}
	return err
}

func (f *CodeFile) Run(ch chan Exit) {
	cmd := MakeCmd(path.Join("bin", f.pkg))
	if f.identity != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%s", path.Join("ids", f.identity)))
	}
	f.proc = cmd
	err := cmd.Run()
	ch <- Exit{f.pkg, err}
}

func (id Identity) Create() error {
	args := make([]string, 0, 10)
	args = append(args, "--name", id.Name)
	if id.Blesser != "" {
		args = append(args, "--blesser", path.Join("ids", id.Blesser))
	}
	if id.Duration != "" {
		args = append(args, "--duration", id.Duration)
	}
	cmd := MakeCmd("identity", args...)
	out, err := os.Create(path.Join("ids", id.Name))
	if err != nil {
		return err
	}
	defer out.Close()
	cmd.Stdout = out
	return cmd.Run()
}

func StartMount() (err error) {
	reader, writer := io.Pipe()
	cmd := MakeCmd("mounttabled")
	cmd.Stdout = writer
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	if err != nil {
		return err
	}
	buf := bufio.NewReader(reader)
	pat := regexp.MustCompile("Mount table at: (.+)\n")

	timeout := time.After(RUN_TIMEOUT)
	ch := make(chan string)
	go (func() {
		for line, err := buf.ReadString('\n'); err == nil; line, err = buf.ReadString('\n') {
			if groups := pat.FindStringSubmatch(line); groups != nil {
				ch <- groups[1]
			}
		}
		close(ch)
	})()
	select {
	case <-timeout:
		log.Fatal("Timeout starting mounttabled")
	case endpoint := <-ch:
		if endpoint == "" {
			log.Fatal("mounttable died")
		}
		Log("mount at ", endpoint)
		return os.Setenv("NAMESPACE_ROOT", endpoint)
	}
	return err
}
