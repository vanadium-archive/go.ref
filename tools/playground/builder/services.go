// Functions to start services needed by the Veyron playground.
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"strconv"
	"time"
)

var (
	proxyPort    = 1234
	proxyName    = "proxy"
	wsprBasePort = 1235
)

// startMount starts a mounttabled process, and sets the NAMESPACE_ROOT env
// variable to the mounttable's location.  We run one mounttabled process for
// the entire environment.
func startMount(timeLimit time.Duration) (proc *os.Process, err error) {
	reader, writer := io.Pipe()
	cmd := makeCmdJsonEvent("", "mounttabled")
	cmd.Stdout = writer
	cmd.Stderr = cmd.Stdout
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	buf := bufio.NewReader(reader)
	// TODO(nlacasse): Find a better way to get the mounttable endpoint.
	pat := regexp.MustCompile("Mount table .+ endpoint: (.+)\n")

	timeout := time.After(timeLimit)
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
		return cmd.Process, os.Setenv("NAMESPACE_ROOT", endpoint)
	}
	return cmd.Process, err
}

// startProxy starts a proxyd process.  We run one proxyd process for the
// entire environment.
func startProxy() (proc *os.Process, err error) {
	cmd := makeCmdJsonEvent("", "proxyd", "-name="+proxyName, "-address=:"+strconv.Itoa(proxyPort))
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	return cmd.Process, err
}

// startWspr starts a wsprd process. We run one wsprd process for each
// javascript file being run. The 'index' argument is used to pick a distinct
// port for each wsprd process.
func startWspr(f *codeFile) (proc *os.Process, port int, err error) {
	port = wsprBasePort + f.index
	cmd := makeCmdJsonEvent(f.Name,
		"wsprd",
		"-v=-1",
		"-vproxy="+proxyName,
		"-port="+strconv.Itoa(port),
		// The identd server won't be used, so pass a fake name.
		"-identd=/unused")

	if f.identity != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("VEYRON_IDENTITY=%s", path.Join("ids", f.identity)))
	}
	err = cmd.Start()
	if err != nil {
		return nil, 0, err
	}
	return cmd.Process, port, err
}
