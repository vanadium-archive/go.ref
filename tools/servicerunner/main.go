// This binary starts several services (mount table, proxy, wspr), then prints a
// JSON map with their vars to stdout (as a single line), then waits forever.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	_ "veyron.io/veyron/veyron/profiles"
)

func panicOnError(err error) {
	if err != nil {
		panic(err)
	}
}

// updateVars captures the vars from the given Handle's stdout and adds them to
// the given vars map, overwriting existing entries.
func updateVars(h modules.Handle, vars map[string]string, varNames ...string) error {
	varsToAdd := map[string]bool{}
	for _, v := range varNames {
		varsToAdd[v] = true
	}
	numLeft := len(varsToAdd)

	s := expect.NewSession(nil, h.Stdout(), 10*time.Second)
	for {
		l := s.ReadLine()
		if err := s.OriginalError(); err != nil {
			return err // EOF or otherwise
		}
		parts := strings.Split(l, "=")
		if len(parts) != 2 {
			return fmt.Errorf("Unexpected line: %s", l)
		}
		if _, ok := varsToAdd[parts[0]]; ok {
			numLeft--
			vars[parts[0]] = parts[1]
			if numLeft == 0 {
				break
			}
		}
	}
	return nil
}

func main() {
	rt.Init()

	if modules.IsModulesProcess() {
		panicOnError(modules.Dispatch())
		return
	}

	vars := map[string]string{}

	sh := modules.NewShell()
	defer sh.Cleanup(os.Stderr, os.Stderr)
	// NOTE(sadovsky): Shell only does this for tests. It would be better if it
	// either always did it or never did it.
	if os.Getenv(consts.VeyronCredentials) == "" {
		panicOnError(sh.CreateAndUseNewCredentials())
		v, ok := sh.GetVar(consts.VeyronCredentials)
		if !ok {
			panic("Missing " + consts.VeyronCredentials)
		}
		vars[consts.VeyronCredentials] = v
	}

	h, err := sh.Start("root", nil, "--", "--veyron.tcp.address=127.0.0.1:0")
	panicOnError(err)
	updateVars(h, vars, "MT_NAME")

	// Set consts.NamespaceRootPrefix env var, consumed downstream by proxyd
	// among others.
	// NOTE(sadovsky): If this var is not set, proxyd takes several seconds to
	// start; if it is set, proxyd starts instantly. Confusing.
	sh.SetVar(consts.NamespaceRootPrefix, vars["MT_NAME"])

	// NOTE(sadovsky): The proxyd binary requires --protocol and --address flags
	// while the proxyd command instead uses ListenSpec flags.
	h, err = sh.Start("proxyd", nil, "--", "--veyron.tcp.address=127.0.0.1:0", "test/proxy")
	panicOnError(err)
	updateVars(h, vars, "PROXY_ADDR")

	h, err = sh.Start("wsprd", nil, "--", "--veyron.tcp.address=127.0.0.1:0", "--veyron.proxy=test/proxy", "--identd=test/identd")
	panicOnError(err)
	updateVars(h, vars, "WSPR_ADDR")

	bytes, err := json.Marshal(vars)
	panicOnError(err)
	fmt.Println(string(bytes))

	// Wait to be killed.
	select {}
}
