package common

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	rps "veyron/examples/rockpaperscissors"

	"veyron2/naming"
	"veyron2/vlog"
)

type Counter struct {
	value int64
	// TODO(rthellend): Figure out why sync/atomic doesn't work properly on armv6l.
	lock sync.Mutex
}

func (c *Counter) Add(delta int64) int64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.value += delta
	return c.value
}

func (c *Counter) Value() int64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.value
}

// FindJudge returns a random rock-paper-scissors judge from the mount table.
func FindJudge(mt naming.MountTable) (string, error) {
	judges, err := findAll(mt, "judge")
	if err != nil {
		return "", err
	}
	if len(judges) > 0 {
		return judges[rand.Intn(len(judges))], nil
	}
	return "", errors.New("no judges")
}

// FindPlayer returns a random rock-paper-scissors player from the mount table.
func FindPlayer(mt naming.MountTable) (string, error) {
	players, err := findAll(mt, "player")
	if err != nil {
		return "", err
	}
	if len(players) > 0 {
		return players[rand.Intn(len(players))], nil
	}
	return "", errors.New("no players")
}

// FindScoreKeepers returns all the rock-paper-scissors score keepers from the
// mount table.
func FindScoreKeepers(mt naming.MountTable) ([]string, error) {
	sKeepers, err := findAll(mt, "scorekeeper")
	if err != nil {
		return nil, err
	}
	return sKeepers, nil
}

func findAll(mt naming.MountTable, t string) ([]string, error) {
	start := time.Now()
	c, err := mt.Glob("rps/" + t + "/*")
	if err != nil {
		vlog.Infof("mt.Glob failed: %v", err)
		return nil, err
	}
	var servers []string
	for e := range c {
		if len(e.Servers) > 0 {
			servers = append(servers, pickBestServer(e.Servers))
		}
	}
	vlog.Infof("findAll(%q) elapsed: %s", t, time.Now().Sub(start))
	return servers, nil
}

// pickBestServer returns the server with the highest TTL.
func pickBestServer(servers []naming.MountedServer) string {
	var server string
	var max time.Duration
	for _, x := range servers {
		if x.TTL > max {
			max = x.TTL
			server = x.Server
		}
	}
	return server
}

func FormatScoreCard(score rps.ScoreCard) string {
	buf := bytes.NewBufferString("")
	var gameType string
	switch score.Opts.GameType {
	case rps.Classic:
		gameType = "Classic"
	case rps.LizardSpock:
		gameType = "LizardSpock"
	default:
		gameType = "Unknown"
	}
	fmt.Fprintf(buf, "Game Type: %s\n", gameType)
	fmt.Fprintf(buf, "Number of rounds: %d\n", score.Opts.NumRounds)
	fmt.Fprintf(buf, "Judge: %s\n", score.Judge)
	fmt.Fprintf(buf, "Player 1: %s\n", score.Players[0])
	fmt.Fprintf(buf, "Player 2: %s\n", score.Players[1])
	for i, r := range score.Rounds {
		roundOffset := time.Duration(r.StartTimeNS - score.StartTimeNS)
		roundTime := time.Duration(r.EndTimeNS - r.StartTimeNS)
		fmt.Fprintf(buf, "Round %2d: Player 1 played %-10q. Player 2 played %-10q. Winner: %d [%-10s/%-10s]\n",
			i+1, r.Moves[0], r.Moves[1], r.Winner, roundOffset, roundTime)
	}
	fmt.Fprintf(buf, "Winner: %d\n", score.Winner)
	fmt.Fprintf(buf, "Time: %s\n", time.Duration(score.EndTimeNS-score.StartTimeNS))
	return buf.String()
}
