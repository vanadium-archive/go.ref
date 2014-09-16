// Functions to start services needed by the Veyron playground.
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

var (
	proxyName = "proxy"
)

// Note: This was copied from veyron/go/src/veyron/tools/findunusedport.
// I would like to be able to import that package directly, but it defines a
// main(), so can't be imported.  An alternative solution would be to call the
// 'findunusedport' binary, but that would require starting another process and
// parsing the output.  It seemed simpler to just copy the function here.
func findUnusedPort() (int, error) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 1000; i++ {
		port := 1024 + rnd.Int31n(64512)
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
		if err != nil {
			continue
		}
		sa := &syscall.SockaddrInet4{Port: int(port)}
		if err := syscall.Bind(fd, sa); err != nil {
			continue
		}
		syscall.Close(fd)
		return int(port), nil
	}
	return 0, fmt.Errorf("Can't find unused port.")
}

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
	port, err := findUnusedPort()
	if err != nil {
		return nil, err
	}
	cmd := makeCmdJsonEvent("", "proxyd", "-name="+proxyName, "-address=localhost:"+strconv.Itoa(port))
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	return cmd.Process, err
}

// startWspr starts a wsprd process. We run one wsprd process for each
// javascript file being run.
func startWspr(f *codeFile) (proc *os.Process, port int, err error) {
	port, err = findUnusedPort()
	if err != nil {
		return nil, port, err
	}
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
