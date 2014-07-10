package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

type Event struct {
	Delay   int
	Message string
}

type Response struct {
	Errors string
	Events []Event
}

// This channel is closed when the server begins shutting down.
// No values are ever sent to it.
var lameduck chan bool = make(chan bool)

func healthz(w http.ResponseWriter, r *http.Request) {
	select {
	case <-lameduck:
		w.WriteHeader(http.StatusInternalServerError)
	default:
		w.Write([]byte("OK"))
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id := <-uniq
	cmd := Docker("run", "-i", "--name", id, "playground")
	cmd.Stdin = r.Body
	buf := new(bytes.Buffer)
	cmd.Stdout = buf
	cmd.Stderr = buf
	// Arbitrary deadline: 2s to compile/start, 1s to run, .5s to shutdown
	timeout := time.After(3500 * time.Millisecond)
	exit := make(chan error)

	go func() { exit <- cmd.Run() }()

	select {
	case <-exit:
	case <-timeout:
		buf.Write([]byte("\nTime exceeded, killing...\n"))
	}
	Docker("rm", "-f", id).Run()

	response := new(Response)
	response.Events = append(response.Events, Event{0, buf.String()})
	body, _ := json.Marshal(response)
	w.Header().Add("Content-Type", "application/json")
	w.Header().Add("Content-Length", fmt.Sprintf("%d", len(body)))
	w.Write(body)
}

func Docker(args ...string) *exec.Cmd {
	full_args := append([]string{"docker"}, args...)
	return exec.Command("sudo", full_args...)
}

// A channel which returns unique ids for the containers.
var uniq = make(chan string)

func init() {
	go func() {
		for i := 0; ; i++ {
			uniq <- fmt.Sprintf("playground_%d", i)
		}
	}()
}

func main() {
	limit_min := 60
	delay_min := limit_min/2 + rand.Intn(limit_min/2)

	// VMs will be periodically killed to prevent any owned vms from
	// causing damage. We want to shutdown cleanly before then so
	// we don't cause requests to fail.
	go WaitForShutdown(time.Minute * time.Duration(delay_min))

	http.HandleFunc("/compile", handler)
	http.HandleFunc("/healthz", healthz)
	http.ListenAndServe(":8181", nil)
}

func WaitForShutdown(limit time.Duration) {
	var beforeExit func() error

	// Shutdown if we get a SIGTERM
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)

	// Or if the time limit expires
	deadline := time.After(limit)
	fmt.Println("Shutting down at", time.Now().Add(limit))
Loop:
	for {
		select {
		case <-deadline:
			// Shutdown the vm.
			fmt.Println("Deadline expired, shutting down.")
			beforeExit = exec.Command("sudo", "halt").Run
			break Loop
		case <-term:
			fmt.Println("Got SIGTERM, shutting down.")
			// VM is probably already shutting down, so just exit.
			break Loop
		}
	}
	// Fail health checks so we stop getting requests.
	close(lameduck)
	// Give running requests time to finish.
	time.Sleep(30 * time.Second)

	// Then go ahead and shutdown.
	if beforeExit != nil {
		err := beforeExit()
		if err != nil {
			panic(err)
		}
	}
	os.Exit(0)
}
