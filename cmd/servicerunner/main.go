// This binary starts several services (mount table, proxy, wspr), then prints a
// JSON map with their vars to stdout (as a single line), then waits forever.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"v.io/v23"

	"v.io/x/ref/lib/flags/consts"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/profiles"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/modules/core"
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

	s := expect.NewSession(nil, h.Stdout(), 30*time.Second)
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
	if modules.IsModulesChildProcess() {
		panicOnError(modules.Dispatch())
		return
	}

	ctx, shutdown := v23.Init()
	defer shutdown()

	vars := map[string]string{}

	sh, err := modules.NewShell(ctx, nil)
	if err != nil {
		panic(fmt.Sprintf("modules.NewShell: %s", err))
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	h, err := sh.Start(core.RootMTCommand, nil, "--veyron.tcp.protocol=ws", "--veyron.tcp.address=127.0.0.1:0")
	panicOnError(err)
	panicOnError(updateVars(h, vars, "MT_NAME"))

	// Set consts.NamespaceRootPrefix env var, consumed downstream.
	sh.SetVar(consts.NamespaceRootPrefix, vars["MT_NAME"])
	v23.GetNamespace(ctx).SetRoots(vars["MT_NAME"])

	proxyShutdown, proxyEndpoint, err := profiles.NewProxy(ctx, "ws", "127.0.0.1:0", "", "test/proxy")
	defer proxyShutdown()
	vars["PROXY_NAME"] = proxyEndpoint.Name()

	h, err = sh.Start(core.WSPRCommand, nil, "--veyron.tcp.protocol=ws", "--veyron.tcp.address=127.0.0.1:0", "--veyron.proxy=test/proxy", "--identd=test/identd")
	panicOnError(err)
	panicOnError(updateVars(h, vars, "WSPR_ADDR"))

	h, err = sh.Start(core.TestIdentitydCommand, nil, "--veyron.tcp.protocol=ws", "--veyron.tcp.address=127.0.0.1:0", "--veyron.proxy=test/proxy", "--host=localhost", "--httpaddr=localhost:0")
	panicOnError(err)
	panicOnError(updateVars(h, vars, "TEST_IDENTITYD_NAME", "TEST_IDENTITYD_HTTP_ADDR"))

	bytes, err := json.Marshal(vars)
	panicOnError(err)
	fmt.Println(string(bytes))

	<-signals.ShutdownOnSignals(ctx)
}
