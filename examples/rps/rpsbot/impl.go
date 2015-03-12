package main

import (
	"errors"

	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
	"v.io/x/ref/examples/rps"
)

// RPS implements rps.RockPaperScissorsServerMethods
type RPS struct {
	player      *Player
	judge       *Judge
	scoreKeeper *ScoreKeeper
	ctx         *context.T
}

func NewRPS(ctx *context.T) *RPS {
	return &RPS{player: NewPlayer(), judge: NewJudge(), scoreKeeper: NewScoreKeeper(), ctx: ctx}
}

func (r *RPS) Judge() *Judge {
	return r.judge
}

func (r *RPS) Player() *Player {
	return r.player
}

func (r *RPS) ScoreKeeper() *ScoreKeeper {
	return r.scoreKeeper
}

func (r *RPS) CreateGame(call ipc.ServerCall, opts rps.GameOptions) (rps.GameId, error) {
	if vlog.V(1) {
		b, _ := security.BlessingNames(call, security.CallSideRemote)
		vlog.Infof("CreateGame %+v from %v", opts, b)
	}
	names, _ := security.BlessingNames(call, security.CallSideLocal)
	if len(names) == 0 {
		return rps.GameId{}, errors.New("no names provided for context")
	}
	return r.judge.createGame(names[0], opts)
}

func (r *RPS) Play(call rps.JudgePlayServerCall, id rps.GameId) (rps.PlayResult, error) {
	names, _ := security.BlessingNames(call, security.CallSideRemote)
	vlog.VI(1).Infof("Play %+v from %v", id, names)
	if len(names) == 0 {
		return rps.PlayResult{}, errors.New("no names provided for context")
	}
	return r.judge.play(call, names[0], id)
}

func (r *RPS) Challenge(call ipc.ServerCall, address string, id rps.GameId, opts rps.GameOptions) error {
	b, _ := security.BlessingNames(call, security.CallSideRemote)
	vlog.VI(1).Infof("Challenge (%q, %+v, %+v) from %v", address, id, opts, b)
	newctx, _ := vtrace.SetNewTrace(r.ctx)
	return r.player.challenge(newctx, address, id, opts)
}

func (r *RPS) Record(call ipc.ServerCall, score rps.ScoreCard) error {
	b, _ := security.BlessingNames(call, security.CallSideRemote)
	vlog.VI(1).Infof("Record (%+v) from %v", score, b)
	return r.scoreKeeper.Record(call, score)
}
