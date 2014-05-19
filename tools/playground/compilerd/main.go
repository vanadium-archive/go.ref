package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
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
	http.HandleFunc("/compile", handler)
	http.ListenAndServe(":8181", nil)
}
