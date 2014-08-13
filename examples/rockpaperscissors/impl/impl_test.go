package impl_test

import (
	"testing"

	rps "veyron/examples/rockpaperscissors"
	"veyron/examples/rockpaperscissors/impl"
	mtlib "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

func startMountTable(t *testing.T, runtime veyron2.Runtime) (string, func()) {
	server, err := runtime.NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dispatcher, err := mtlib.NewMountTable("")

	protocol, hostname := "tcp", "127.0.0.1:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	if err := server.Serve("", dispatcher); err != nil {
		t.Fatalf("Serve(%v) failed: %v", dispatcher, err)
	}
	address := naming.JoinAddressName(endpoint.String(), "")
	vlog.VI(1).Infof("Mount table running at endpoint: %s", address)
	return address, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startRockPaperScissors(t *testing.T, rt veyron2.Runtime, mtAddress string) (*impl.RPS, func()) {
	ns := rt.Namespace()
	ns.SetRoots(mtAddress)
	server, err := rt.NewServer()
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	rpsService := impl.NewRPS()

	if _, err = server.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	disp := ipc.SoloDispatcher(rps.NewServerRockPaperScissors(rpsService), nil)
	names := []string{"rps/judge/test", "rps/player/test", "rps/scorekeeper/test"}
	for _, n := range names {
		if err := server.Serve(n, disp); err != nil {
			t.Fatalf("Serve(%v) failed: %v", n, err)
		}
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
	runtime := rt.Init()
	defer runtime.Cleanup()
	mtAddress, mtStop := startMountTable(t, runtime)
	defer mtStop()
	rpsService, rpsStop := startRockPaperScissors(t, runtime, mtAddress)
	defer rpsStop()

	const numGames = 10
	for x := 0; x < numGames; x++ {
		if err := rpsService.Player().InitiateGame(); err != nil {
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
}
