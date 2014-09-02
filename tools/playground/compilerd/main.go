package main

import (
	"bytes"
	"encoding/json"
	"flag"
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

var (
	// This channel is closed when the server begins shutting down.
	// No values are ever sent to it.
	lameduck chan bool = make(chan bool)

	address = flag.String("address", ":8181", "address to listen on")

	// Note, shutdown triggers on SIGTERM or when the time limit is hit.
	shutdown = flag.Bool("shutdown", true, "whether to ever shutdown the machine")
)

func healthz(w http.ResponseWriter, r *http.Request) {
	select {
	case <-lameduck:
		w.WriteHeader(http.StatusInternalServerError)
	default:
		w.Write([]byte("OK"))
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	// CORS headers.
	// TODO(nlacasse): Fill the origin header in with actual playground origin
	// before going to production.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding")

	// CORS sends an OPTIONS pre-flight request to make sure the request will be
	// allowed.
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

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
	// Arbitrary deadline: 2s to compile/start, 1s to run, .5s to shutdown.
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
	flag.Parse()

	if *shutdown {
		limit_min := 60
		delay_min := limit_min/2 + rand.Intn(limit_min/2)

		// VMs will be periodically killed to prevent any owned VMs from causing
		// damage. We want to shutdown cleanly before then so we don't cause
		// requests to fail.
		go WaitForShutdown(time.Minute * time.Duration(delay_min))
	}

	http.HandleFunc("/compile", handler)
	http.HandleFunc("/healthz", healthz)

	fmt.Printf("Serving %s\n", *address)
	http.ListenAndServe(*address, nil)
}

func WaitForShutdown(limit time.Duration) {
	var beforeExit func() error

	// Shutdown if we get a SIGTERM.
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)

	// Or if the time limit expires.
	deadline := time.After(limit)
	fmt.Println("Shutting down at", time.Now().Add(limit))
Loop:
	for {
		select {
		case <-deadline:
			// Shutdown the VM.
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
