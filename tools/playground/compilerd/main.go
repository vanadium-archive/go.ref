package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/groupcache/lru"

	"veyron.io/veyron/veyron/tools/playground/event"
)

type ResponseBody struct {
	Errors string
	Events []event.Event
}

type CachedResponse struct {
	Status int
	Body   ResponseBody
}

var (
	// This channel is closed when the server begins shutting down.
	// No values are ever sent to it.
	lameduck chan bool = make(chan bool)

	address = flag.String("address", ":8181", "address to listen on")

	// Note, shutdown triggers on SIGTERM or when the time limit is hit.
	shutdown = flag.Bool("shutdown", true, "whether to ever shutdown the machine")

	// Maximum request and response size. Same limit as imposed by Go tour.
	maxSize = 1 << 16

	// In-memory LRU cache of request/response bodies. Keys are sha1 sum of
	// request bodies (20 bytes each), values are of type CachedResponse.
	// NOTE(nlacasse): The cache size (10k) was chosen arbitrarily and should
	// perhaps be optimized.
	cache = lru.New(10000)
)

func healthz(w http.ResponseWriter, r *http.Request) {
	select {
	case <-lameduck:
		w.WriteHeader(http.StatusInternalServerError)
	default:
		w.Write([]byte("ok"))
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

	requestBody := streamToBytes(r.Body)

	if len(requestBody) > maxSize {
		responseBody := new(ResponseBody)
		responseBody.Errors = "Program too large."
		respondWithBody(w, http.StatusBadRequest, responseBody)
		return
	}

	// Hash the body and see if it's been cached. If so, return the cached
	// response status and body.
	// NOTE(sadovsky): In the client we may shift timestamps (based on current
	// time) and introduce a fake delay.
	requestBodyHash := sha1.Sum(requestBody)
	if cachedResponse, ok := cache.Get(requestBodyHash); ok {
		if cachedResponseStruct, ok := cachedResponse.(CachedResponse); ok {
			respondWithBody(w, cachedResponseStruct.Status, cachedResponseStruct.Body)
			return
		} else {
			log.Printf("Invalid cached response: %v\n", cachedResponse)
			cache.Remove(requestBodyHash)
		}
	}

	// TODO(nlacasse): It would be cool if we could stream the output
	// messages while the process is running, rather than waiting for it to
	// exit and dumping all the output then.

	id := <-uniq

	// TODO(sadovsky): Set runtime constraints on CPU and memory usage.
	// http://docs.docker.com/reference/run/#runtime-constraints-on-cpu-and-memory
	cmd := Docker("run", "-i", "--name", id, "playground")
	cmd.Stdin = bytes.NewReader(requestBody)

	// Builder will return all normal output as json events on stdout, and will
	// return unexpected errors on stderr.
	stdoutBuf, stderrBuf := new(bytes.Buffer), new(bytes.Buffer)
	cmd.Stdout, cmd.Stderr = stdoutBuf, stderrBuf

	// Arbitrary deadline (enough to compile, run, shutdown).
	timeout := time.After(5000 * time.Millisecond)
	exit := make(chan error)

	go func() { exit <- cmd.Run() }()

	select {
	case <-exit:
	case <-timeout:
		// TODO(sadovsky): Race condition. More output could show up after this
		// message.
		stderrBuf.Write([]byte("\nTime exceeded, killing...\n"))
	}

	// TODO(nlacasse): This takes a long time, during which the client is waiting
	// for a response.  I tried moving it to after the response is sent, but a
	// subsequent request will trigger a new "docker run", which somehow has to
	// wait for this "docker rm" to finish.  This caused some requests to timeout
	// unexpectedly.
	//
	// We should figure out a better way to run this, so that we can return
	// quickly, and not mess up other requests.
	//
	// Setting GOMAXPROCS may or may not help.  See
	// https://github.com/docker/docker/issues/6480
	Docker("rm", "-f", id).Run()

	// If the response is bigger than the limit, cache the response and return an
	// error.
	if stdoutBuf.Len() > maxSize {
		status := http.StatusBadRequest
		responseBody := new(ResponseBody)
		responseBody.Errors = "Program output too large."
		cache.Add(requestBodyHash, CachedResponse{
			Status: status,
			Body:   *responseBody,
		})
		respondWithBody(w, status, responseBody)
		return
	}

	responseBody := new(ResponseBody)
	// TODO(nlacasse): Make these errors Events, so that we can send them
	// back in the Events array.  This will simplify streaming the events to the
	// client in realtime.
	responseBody.Errors = stderrBuf.String()

	// Decode the json events from stdout and add them to the responseBody.
	for line, err := stdoutBuf.ReadBytes('\n'); err == nil; line, err = stdoutBuf.ReadBytes('\n') {
		var e event.Event
		json.Unmarshal(line, &e)
		responseBody.Events = append(responseBody.Events, e)
	}

	cache.Add(requestBodyHash, CachedResponse{
		Status: http.StatusOK,
		Body:   *responseBody,
	})
	respondWithBody(w, http.StatusOK, responseBody)
}

func respondWithBody(w http.ResponseWriter, status int, body interface{}) {
	bodyJson, _ := json.Marshal(body)
	w.Header().Add("Content-Type", "application/json")
	w.Header().Add("Content-Length", fmt.Sprintf("%d", len(bodyJson)))
	w.Write(bodyJson)

	// TODO(nlacasse): This flush doesn't really help us right now, but
	// we'll definitly need something like it when we switch to the
	// streaming model.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	} else {
		log.Println("Cannot flush.")
	}
}

func streamToBytes(stream io.Reader) []byte {
	buf := new(bytes.Buffer)
	buf.ReadFrom(stream)
	return buf.Bytes()
}

func Docker(args ...string) *exec.Cmd {
	fullArgs := []string{"docker"}
	fullArgs = append(fullArgs, args...)
	return exec.Command("sudo", fullArgs...)
}

// A channel which returns unique ids for the containers.
var uniq = make(chan string)

func init() {
	val := time.Now().UnixNano()
	go func() {
		for {
			uniq <- fmt.Sprintf("playground_%d", val)
			val++
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

	log.Printf("Serving %s\n", *address)
	http.ListenAndServe(*address, nil)
}

func WaitForShutdown(limit time.Duration) {
	var beforeExit func() error

	// Shutdown if we get a SIGTERM.
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)

	// Or if the time limit expires.
	deadline := time.After(limit)
	log.Println("Shutting down at", time.Now().Add(limit))
Loop:
	for {
		select {
		case <-deadline:
			// Shutdown the VM.
			log.Println("Deadline expired, shutting down.")
			beforeExit = exec.Command("sudo", "halt").Run
			break Loop
		case <-term:
			log.Println("Got SIGTERM, shutting down.")
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
