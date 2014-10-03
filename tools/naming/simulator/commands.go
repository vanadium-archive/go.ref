package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"veyron.io/veyron/veyron/lib/modules"
)

type builtinCmd func(sh *modules.Shell, state *cmdState, args ...string) (string, error)

var varRE = regexp.MustCompile("(.*)=(.*)")

var builtins = map[string]*struct {
	nargs       int
	usage       string
	needsHandle bool
	fn          builtinCmd
}{
	"print":  {-1, "print <args>...", false, print},
	"help":   {-1, "help", false, nil},
	"set":    {-1, "set <var>=<val>...", false, set},
	"assert": {2, "val1 val2", false, assert},
	"read":   {-1, "read <handle> [var]", true, read},
	"eval":   {1, "eval <handle>", true, eval},
	"wait":   {1, "wait <handle>", true, wait},
	"stop":   {1, "stop <handle>", true, stop},
	"list":   {0, "list", false, list},
	"quit":   {0, "quit", false, quit},
}

func init() {
	builtins["help"].fn = help
}

func print(_ *modules.Shell, _ *cmdState, args ...string) (string, error) {
	r := strings.Join(args, " ")
	return r, nil
}

func help(sh *modules.Shell, _ *cmdState, args ...string) (string, error) {
	r := ""
	if len(args) == 0 {
		for k, _ := range builtins {
			if k == "help" {
				continue
			}
			r += k + ", "
		}
		r += sh.String()
		return r, nil
	} else {
		for _, a := range args {
			if v := builtins[a]; v != nil {
				r += v.usage + "\n"
				continue
			}
			h := sh.Help(a)
			if len(h) == 0 {
				return "", fmt.Errorf("unknown command: %q", a)
			} else {
				r += h
			}
		}
	}
	return r, nil
}

func parseVar(expr string) (string, string, error) {
	m := varRE.FindAllStringSubmatch(expr, 1)
	if len(m) != 1 || len(m[0]) != 3 {
		return "", "", fmt.Errorf("%q is not an assignment statement", expr)
	}
	return m[0][1], m[0][2], nil
}

func set(sh *modules.Shell, _ *cmdState, args ...string) (string, error) {
	r := ""
	if len(args) == 0 {
		for _, v := range sh.Env() {
			r += v + "\n"
		}
		return r, nil
	}
	for _, a := range args {
		k, v, err := parseVar(a)
		if err != nil {
			return "", err
		}
		sh.SetVar(k, v)
	}
	return "", nil
}

func assert(sh *modules.Shell, _ *cmdState, args ...string) (string, error) {
	if args[0] != args[1] {
		return "", fmt.Errorf("assertion failed: %q != %q", args[0], args[1])
	}
	return "", nil
}

func readStderr(state *cmdState) (string, error) {
	var b bytes.Buffer
	if err := state.Handle.Shutdown(nil, &b); err != nil && err != io.EOF {
		return b.String(), err
	}
	return b.String(), nil
}

func handleWrapper(sh *modules.Shell, fn builtinCmd, args ...string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("missing handle argument")
	}
	state := handles[args[0]]
	if state == nil {
		return "", fmt.Errorf("invalid handle")
	}
	errstr := ""
	r, err := fn(sh, state, args...)
	if err != nil {
		errstr, _ = readStderr(state)
		errstr = strings.TrimSuffix(errstr, "\n")
		if len(errstr) > 0 {
			err = fmt.Errorf("%s: %v", errstr, err)
		}
	}
	return r, err
}

func read(sh *modules.Shell, state *cmdState, args ...string) (string, error) {
	l := state.Session.ReadLine()
	for _, a := range args[1:] {
		sh.SetVar(a, l)
	}
	return l, state.Session.OriginalError()
}

func eval(sh *modules.Shell, state *cmdState, args ...string) (string, error) {
	l := state.Session.ReadLine()
	if err := state.Session.OriginalError(); err != nil {
		return l, err
	}
	k, v, err := parseVar(l)
	if err != nil {
		return "", err
	}
	sh.SetVar(k, v)
	return l, nil
}

func stop(sh *modules.Shell, state *cmdState, args ...string) (string, error) {
	state.Handle.CloseStdin()
	return wait(sh, state, args...)
}

func wait(sh *modules.Shell, state *cmdState, args ...string) (string, error) {
	// Read and return stdout
	r, err := state.Session.Finish(nil)
	delete(handles, args[0])
	if err != nil {
		return r, err
	}
	// Now read and return the contents of stderr as e
	if str, err := readStderr(state); err != nil && err != io.EOF {
		return str, err
	}
	return r, nil
}

func list(sh *modules.Shell, _ *cmdState, args ...string) (string, error) {
	r := ""
	for h, v := range handles {
		r += h + ": " + v.line + "\n"
	}
	return r, nil
}

func quit(sh *modules.Shell, _ *cmdState, args ...string) (string, error) {
	r := ""
	for k, h := range handles {
		if err := h.Handle.Shutdown(os.Stdout, os.Stdout); err != nil {
			r += fmt.Sprintf("%s: %v\n", k, err)
		} else {
			r += fmt.Sprintf("%s: ok\n", k)
		}
	}
	fmt.Fprintf(os.Stdout, r)
	os.Exit(0)
	panic("unreachable")
}

func getLine(sh *modules.Shell, args ...string) (string, error) {
	handle := handles[args[0]]
	if handle == nil {
		return "", fmt.Errorf("invalid handle")
	}
	l := handle.Session.ReadLine()
	return l, handle.Session.Error()
}
