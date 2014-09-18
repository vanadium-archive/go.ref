// Package modules defines an interface and support for running common
// Veyron services and functions - collectively called 'modules'. Modules
// may be implemented as libraries or subprocesses.
//
// This package also provides implementations of this interface for a variety of
// common Veyron services and functions such as naming.
package modules

import (
	"flag"
	"os"
	"strings"

	"veyron.io/veyron/veyron/lib/testutil/blackbox"
)

func InModule() {
	flag.Parse()
	// NASTY - fix this to hide the implementation details of modules..
	if def := flag.Lookup("subcommand"); def != nil {
		if def.Value.String() != "" {
			blackbox.HelperProcess(nil)
			os.Exit(0)
		}
	}
}

// Handle represents the state of an active module.
type Handle interface {
	// Stop terminates the execution of the module and prints any error output
	// to stderr. It will return an error if the module failed to terminate
	// cleanly.
	Stop() error
}

// A map of name/value pairs returned as output from a module
type Variables map[string]string

// T is the interface that all modules must implement.
type T interface {
	// Run, runs the module with the specified arguments and return an instance
	// of Variables and a string slice of text output. The Handle return value
	// will be set if the module is still active or nil otherwise.
	// An error is returned if the module failed to start or execute.
	Run(args []string) (Variables, []string, Handle, error)

	// Help returns a string explaining how to use the module
	Help() string

	// Daemon returns true if the module is a subprocess that continues
	// running after Run has returned. The Stop method on Handle should
	// be called to terminate the subprocess and collect any error output
	// from it. Alternatively the Cleanup function may be called to
	// clean up the state on all currently running subprocess modules.
	Daemon() bool
}

// Cleanup cleanups up any subprocess modules that are still running.
func Cleanup() {
	children.Lock()
	if children.cleaningUp {
		children.Unlock()
		return
	}
	children.cleaningUp = true
	var toclean []Handle
	for k, _ := range children.children {
		toclean = append(toclean, k)
	}
	children.Unlock()
	for _, h := range toclean {
		h.Stop()
	}
}

// Update adds the key, val pair to its receiver, overwriting an existing value.
func (vars Variables) Update(key, val string) {
	vars[key] = val
}

// UpdateFromString parses string for a key=val pair and adds it to its
// receiver, overwriting an existing value.
func (vars Variables) UpdateFromString(arg string) (string, string) {
	if i := strings.Index(arg, "="); i != -1 {
		key := arg[0:i]
		val := arg[i+1:]
		vars[key] = val
		return key, val
	}
	return "", ""
}

// UpdateFromVariables adds all of the key, value pairs in newVars to its
// receiver, overwriting any existing values.
func (vars Variables) UpdateFromVariables(newVars Variables) {
	for k, v := range newVars {
		vars[k] = v
	}
}
