package modules

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"v.io/v23/vlog"

	vexec "v.io/core/veyron/lib/exec"
)

type commandDesc struct {
	factory func() command
	main    Main
	help    string
}

type cmdRegistry struct {
	sync.Mutex
	cmds map[string]*commandDesc
}

var registry = &cmdRegistry{cmds: make(map[string]*commandDesc)}

func (r *cmdRegistry) addCommand(name, help string, factory func() command, main Main) {
	r.Lock()
	defer r.Unlock()
	r.cmds[name] = &commandDesc{factory, main, help}
}

func (r *cmdRegistry) getCommand(name string) *commandDesc {
	r.Lock()
	defer r.Unlock()
	return r.cmds[name]
}

// RegisterChild adds a new command to the registry that will be run
// as a subprocess. It must be called before Dispatch or DispatchInTest is
// called, typically from an init function.
func RegisterChild(name, help string, main Main) {
	factory := func() command { return newExecHandle(name) }
	registry.addCommand(name, help, factory, main)
}

// RegisterFunction adds a new command to the registry that will be run
// within the current process. It can be called at any time prior to an
// attempt to use it.
func RegisterFunction(name, help string, main Main) {
	factory := func() command { return newFunctionHandle(name, main) }
	registry.addCommand(name, help, factory, main)
}

// Help returns the help message for the specified command, or a list
// of all commands if the command parameter is an empty string.
func Help(command string) string {
	return registry.help(command)
}

func (r *cmdRegistry) help(command string) string {
	r.Lock()
	defer r.Unlock()
	if len(command) == 0 {
		h := ""
		for c, _ := range r.cmds {
			h += c + ", "
		}
		return strings.TrimRight(h, ", ")
	}
	if c := r.cmds[command]; c != nil {
		return command + ": " + c.help
	}
	return ""
}

const shellEntryPoint = "VEYRON_SHELL_HELPER_PROCESS_ENTRY_POINT"

// IsModulesProcess returns true if this process is run using
// the modules package.
func IsModulesProcess() bool {
	return os.Getenv(shellEntryPoint) != ""
}

// SetEntryPoint returns a slice of strings that is guaranteed to contain
// one and only instance of the entry point environment variable set to the
// supplied name value.
func SetEntryPoint(env map[string]string, name string) []string {
	newEnv := make([]string, 0, len(env)+1)
	for k, v := range env {
		if k == shellEntryPoint {
			continue
		}
		newEnv = append(newEnv, k+"="+v)
	}
	return append(newEnv, shellEntryPoint+"="+name)
}

// Dispatch will execute the requested subprocess command from a within a
// a subprocess. It will return without an error if it is executed by a
// process that does not specify an entry point in its environment.
//
// func main() {
//     if modules.IsModulesProcess() {
//         if err := modules.Dispatch(); err != nil {
//             panic("error")
//         }
//         eturn
//     }
//     parent code...
//
func Dispatch() error {
	if !IsModulesProcess() {
		return nil
	}
	return registry.dispatch()
}

// TODO(cnicolaou): delete this in a subsequent CL.
// DispatchInTest will execute the requested subproccess command from within
// a unit test run as a subprocess.
func DispatchInTest() {
	if !IsTestHelperProcess() {
		return
	}
	if err := registry.dispatch(); err != nil {
		vlog.Fatalf("Failed: %s", err)
	}
	os.Exit(0)
}

// DispatchAndExit is like Dispatch except that it will call os.Exit(0)
// when executed within a child process and the command succeeds, or panic
// on encountering an error.
//
// func main() {
//     modules.DispatchAndExit()
//     parent code...
//
func DispatchAndExit() {
	if !IsModulesProcess() {
		return
	}
	if IsTestHelperProcess() {
		panic("use DispatchInTest in unittests")
	}
	if err := registry.dispatch(); err != nil {
		panic(fmt.Sprintf("unexpected error: %s", err))
	}
	os.Exit(0)
}

func (r *cmdRegistry) dispatch() error {
	ch, err := vexec.GetChildHandle()
	if err != nil {
		// This is for debugging only. It's perfectly reasonable for this
		// error to occur if the process is started by a means other
		// than the exec library.
		vlog.VI(1).Infof("failed to get child handle: %s", err)
	}

	// Only signal that the child is ready or failed if we successfully get
	// a child handle. We most likely failed to get a child handle
	// because the subprocess was run directly from the command line.
	command := os.Getenv(shellEntryPoint)
	if len(command) == 0 {
		err := fmt.Errorf("Failed to find entrypoint %q", command)
		if ch != nil {
			ch.SetFailed(err)
		}
		return err
	}

	m := registry.getCommand(command)
	if m == nil {
		err := fmt.Errorf("%s: not registered", command)
		if ch != nil {
			ch.SetFailed(err)
		}
		return err
	}

	if ch != nil {
		ch.SetReady()
	}

	go func(pid int) {
		for {
			_, err := os.FindProcess(pid)
			if err != nil {
				vlog.Fatalf("Looks like our parent exited: %v", err)
			}
			time.Sleep(time.Second)
		}
	}(os.Getppid())

	flag.Parse()
	return m.main(os.Stdin, os.Stdout, os.Stderr, envSliceToMap(os.Environ()), flag.Args()...)
}

// WaitForEOF returns when a read on its io.Reader parameter returns io.EOF
func WaitForEOF(stdin io.Reader) {
	buf := [1024]byte{}
	for {
		if _, err := stdin.Read(buf[:]); err == io.EOF {
			return
		}
	}
}
