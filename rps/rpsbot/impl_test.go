package main

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strconv"
	"testing"

	"v.io/apps/rps"

	mtlib "v.io/core/veyron/services/mounttable/lib"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/vlog"
)

var spec = ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}

func startMountTable(t *testing.T, ctx *context.T) (string, func()) {
	server, err := veyron2.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dispatcher, err := mtlib.NewMountTable("")

	endpoints, err := server.Listen(spec)
	if err != nil {
		t.Fatalf("Listen(%v) failed: %v", spec, err)
	}
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Fatalf("ServeDispatcher(%v) failed: %v", dispatcher, err)
	}
	address := naming.JoinAddressName(endpoints[0].String(), "")
	vlog.VI(1).Infof("Mount table running at endpoint: %s", address)
	return address, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startRockPaperScissors(t *testing.T, ctx *context.T, mtAddress string) (*RPS, func()) {
	ns := veyron2.GetNamespace(ctx)
	ns.SetRoots(mtAddress)
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	rpsService := NewRPS(ctx)

	if _, err = server.Listen(spec); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	names := []string{"rps/judge/test", "rps/player/test", "rps/scorekeeper/test"}
	if err := server.Serve(names[0], rps.RockPaperScissorsServer(rpsService), nil); err != nil {
		t.Fatalf("Serve(%v) failed: %v", names[0], err)
	}
	for _, n := range names[1:] {
		server.AddName(n)
	}
	return rpsService, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

// TestRockPaperScissorsImpl runs one rock-paper-scissors game and verifies
// that all the counters are consistent.
func TestRockPaperScissorsImpl(t *testing.T) {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	mtAddress, mtStop := startMountTable(t, ctx)
	defer mtStop()
	rpsService, rpsStop := startRockPaperScissors(t, ctx, mtAddress)
	defer rpsStop()

	const numGames = 100
	for x := 0; x < numGames; x++ {
		if err := rpsService.Player().InitiateGame(ctx); err != nil {
			t.Errorf("Failed to initiate game: %v", err)
		}
	}
	rpsService.Player().WaitUntilIdle()

	// For each game, the player plays twice. So, we expect the player to
	// show that it played 2×numGames, and won numGames.
	played, won := rpsService.Player().Stats()
	if want, got := int64(2*numGames), played; want != got {
		t.Errorf("Unexpected number of played games. Got %d, want %d", got, want)
	}
	if want, got := int64(numGames), won; want != got {
		t.Errorf("Unexpected number of won games. Got %d, want %d", got, want)
	}

	// The Judge ran every game.
	if want, got := int64(numGames), rpsService.Judge().Stats(); want != got {
		t.Errorf("Unexpected number of games run. Got %d, want %d", got, want)
	}

	// The Score Keeper received one score card per game.
	if want, got := int64(numGames), rpsService.ScoreKeeper().Stats(); want != got {
		t.Errorf("Unexpected number of score cards. Got %d, want %d", got, want)
	}

	// Check for leaked goroutines.
	if n := runtime.NumGoroutine(); n > 100 {
		t.Logf("Num goroutines: %d", n)
		if leak := reportLeakedGoroutines(t, n/3); len(leak) != 0 {
			t.Errorf("Potentially leaked goroutines:\n%s", leak)
		}
	}
}

// reportLeakedGoroutines reads the goroutine pprof profile and returns the
// goroutines that have more than 'threshold' instances.
func reportLeakedGoroutines(t *testing.T, threshold int) string {
	f, err := ioutil.TempFile("", "test-profile-")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer f.Close()
	defer os.Remove(f.Name())
	p := pprof.Lookup("goroutine")
	if p == nil {
		t.Fatalf("Failed to lookup the goroutine profile")
	}
	if err := p.WriteTo(f, 1); err != nil {
		t.Fatalf("Failed to write profile: %v", err)
	}

	re, err := regexp.Compile(`^([0-9]+) @`)
	if err != nil {
		t.Fatalf("Failed to compile regexp")
	}
	f.Seek(0, 0)
	r := bufio.NewReader(f)

	var buf bytes.Buffer
	var printing bool
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read bytes: %v", err)
		}
		if len(line) <= 1 {
			printing = false
			continue
		}
		if !printing {
			if m := re.FindSubmatch(line); len(m) == 2 {
				count, _ := strconv.Atoi(string(m[1]))
				if count > threshold {
					printing = true
				}
			}
		}
		if printing {
			buf.Write(line)
		}
	}
	return buf.String()
}
