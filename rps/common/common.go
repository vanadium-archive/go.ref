// Package common factors out common utility functions that both the
// rock paper scissors clients and servers invoke.
package common

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"time"

	"v.io/apps/rps"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/vlog"
)

// CreateName creates a name using the username and hostname.
func CreateName() string {
	hostname, err := os.Hostname()
	if err != nil {
		vlog.Fatalf("os.Hostname failed: %v", err)
	}
	return os.Getenv("USER") + "@" + hostname
}

// FindJudge returns a random rock-paper-scissors judge from the mount table.
func FindJudge(ctx *context.T) (string, error) {
	judges, err := findAll(ctx, "judge")
	if err != nil {
		return "", err
	}
	if len(judges) > 0 {
		return judges[rand.Intn(len(judges))], nil
	}
	return "", errors.New("no judges")
}

// FindPlayer returns a random rock-paper-scissors player from the mount table.
func FindPlayer(ctx *context.T) (string, error) {
	players, err := findAll(ctx, "player")
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
func FindScoreKeepers(ctx *context.T) ([]string, error) {
	sKeepers, err := findAll(ctx, "scorekeeper")
	if err != nil {
		return nil, err
	}
	return sKeepers, nil
}

func findAll(ctx *context.T, t string) ([]string, error) {
	start := time.Now()
	ns := veyron2.GetNamespace(ctx)
	c, err := ns.Glob(ctx, "rps/"+t+"/*")
	if err != nil {
		vlog.Infof("mt.Glob failed: %v", err)
		return nil, err
	}
	var servers []string
	for e := range c {
		if e.Error != nil {
			vlog.VI(1).Infof("findAll(%q) error for %q: %v", t, e.Name, e.Error)
			continue
		}
		servers = append(servers, e.Name)
	}
	vlog.VI(1).Infof("findAll(%q) elapsed: %s", t, time.Now().Sub(start))
	return servers, nil
}

// FormatScoreCard returns a string representation of a score card.
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
		fmt.Fprintf(buf, "Round %2d: Player 1 played %-10q. Player 2 played %-10q. Winner: %d %-28s [%-10s/%-10s]\n",
			i+1, r.Moves[0], r.Moves[1], r.Winner, r.Comment, roundOffset, roundTime)
	}
	fmt.Fprintf(buf, "Winner: %d\n", score.Winner)
	fmt.Fprintf(buf, "Time: %s\n", time.Duration(score.EndTimeNS-score.StartTimeNS))
	return buf.String()
}
