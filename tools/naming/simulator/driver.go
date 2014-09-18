// This app provides a simple scripted environment for running common veyron
// services as subprocesses and testing interactions between them. It is
// structured as an interpreter, with global variables and variable
// expansion, but no control flow. The command set that it supports is
// extendable by adding new 'modules' that implement the API defined
// by veyron/lib/testutil/modules.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode"

	"veyron.io/veyron/veyron/lib/testutil/modules"

	"veyron.io/veyron/veyron2/rt"
)

type commandFunc func() modules.T

var (
	commands    map[string]commandFunc
	globals     modules.Variables
	debug       bool
	interactive bool
)

func init() {
	flag.BoolVar(&interactive, "interactive", true, "set interactive/batch mode")
	flag.BoolVar(&debug, "debug", false, "set debug mode")

	commands = make(map[string]commandFunc)

	// We maintaing a single, global, dictionary for variables.
	globals = make(modules.Variables)

	// 'bultins'
	commands["help"] = helpF
	commands["get"] = getF
	commands["set"] = setF
	commands["print"] = printF
	commands["sleep"] = sleepF

	// TODO(cnicolaou): add 'STOP' command to shutdown a running server,
	// need to return the handle and then call Stop on it.

	// modules
	commands["rootMT"] = modules.NewRootMT
	commands["nodeMT"] = modules.NewNodeMT
	commands["setLocalRoots"] = modules.NewSetRoot
	commands["ls"] = modules.NewGlob
	commands["lsat"] = modules.NewGlobAt
	commands["lsmt"] = modules.NewGlobAtMT
	commands["resolve"] = modules.NewResolve
	commands["resolveMT"] = modules.NewResolveMT
	commands["echoServer"] = modules.NewEchoServer
	commands["echo"] = modules.NewEchoClient
	commands["clockServer"] = modules.NewClockServer
	commands["time"] = modules.NewClockClient
}

func prompt(lineno int) {
	if interactive {
		fmt.Printf("%d> ", lineno)
	}
}

func main() {
	modules.InModule()
	rt.Init()

	scanner := bufio.NewScanner(os.Stdin)
	lineno := 1
	prompt(lineno)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#") && len(line) > 0 {
			if line == "eof" {
				break
			}
			if err := process(line, lineno); err != nil {
				if debug {
					fmt.Printf("%d> %s: %v\n", lineno, line, err)
				} else {
					fmt.Printf("%d> %v\n", lineno, err)
				}
			}
		}
		lineno++
		prompt(lineno)
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("error reading input: %v\n", err)
	}

	modules.Cleanup()
}

func process(line string, lineno int) error {
	fields, err := splitQuotedFields(line)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return fmt.Errorf("no input")
	}
	name := fields[0]

	var args []string
	if len(fields) > 1 {
		args = fields[1:]
	} else {
		args = []string{}
	}

	sub, err := subVariables(args, globals)
	if err != nil {
		return err
	}

	factory := commands[name]
	if factory == nil {
		return fmt.Errorf("unrecognised command %q", name)
	}
	if vars, output, _, err := factory().Run(sub); err != nil {
		return err
	} else {
		if debug || interactive {
			fmt.Printf("%d> %s\n", lineno, line)
		}
		if len(output) > 0 {
			if !interactive {
				fmt.Printf("%d> ", lineno)
			}
			fmt.Printf("%s\n", strings.Join(output, " "))
		}
		if debug && len(vars) > 0 {
			for k, v := range vars {
				fmt.Printf("\t%s=%q .... \n", k, v)
			}
			fmt.Println()
		}
		globals.UpdateFromVariables(vars)
	}
	return nil
}

// splitQuotedFields a line into fields, allowing for quoted strings.
func splitQuotedFields(line string) ([]string, error) {
	fields := []string{}
	inquote := false
	var field []rune
	for _, c := range line {
		switch {
		case c == '"':
			if inquote {
				fields = append(fields, string(field))
				field = nil
				inquote = false
			} else {
				inquote = true
			}
		case unicode.IsSpace(c):
			if inquote {
				field = append(field, c)
			} else {
				if len(field) > 0 {
					fields = append(fields, string(field))
				}
				field = nil
			}
		default:
			field = append(field, c)
		}
	}
	if inquote {
		return nil, fmt.Errorf("unterminated quoted input")
	}

	if len(field) > 0 {
		fields = append(fields, string(field))
	}
	return fields, nil
}

// subVariables substitutes variables that occur in the string slice
// args with values from vars.
func subVariables(args []string, vars modules.Variables) ([]string, error) {
	var results []string
	for _, a := range args {
		if r, err := subVariablesInArgument(a, vars); err != nil {
			return results, err
		} else {
			results = append(results, r)
		}
	}
	return results, nil
}

// subVariablesInArgument substitutes variables that occur in the string
// parameter with values from vars.
//
// A variable, is introduced by $, terminated by \t, space, / , : or !.
// Variables may also be enclosed by {} (as in ${VAR}) to allow for embedding
// within strings.
func subVariablesInArgument(a string, vars modules.Variables) (string, error) {
	first := strings.Index(a, "$")
	if first < 0 {
		return a, nil
	}
	parts := strings.Split(a, "$")
	result := parts[0]
	vn := ""
	rem := 0
	for _, p := range parts[1:] {
		start := 0
		end := -1
		if strings.HasPrefix(p, "{") {
			start = 1
			end = strings.Index(p, "}")
			if end < 0 {
				return "", fmt.Errorf("unterminated variable: %q", p)
			}
			rem = end + 1
		} else {
			end = strings.IndexAny(p, "\t/,:! ")
			if end < 0 {
				end = len(p)
			}
			rem = end
		}
		vn = p[start:end]
		r := p[rem:]
		v, present := vars[vn]
		if !present {
			return "", fmt.Errorf("unknown variable: %q", vn)
		}
		result += v
		result += r
	}
	return result, nil
}
