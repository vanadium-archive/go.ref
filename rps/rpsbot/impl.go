package main

import (
	"errors"

	"v.io/apps/rps"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/vlog"
	"v.io/v23/vtrace"
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

func (r *RPS) CreateGame(ctx ipc.ServerContext, opts rps.GameOptions) (rps.GameID, error) {
	if vlog.V(1) {
		b, _ := ctx.RemoteBlessings().ForContext(ctx)
		vlog.Infof("CreateGame %+v from %v", opts, b)
	}
	names, _ := ctx.LocalBlessings().ForContext(ctx)
	if len(names) == 0 {
		return rps.GameID{}, errors.New("no names provided for context")
	}
	return r.judge.createGame(names[0], opts)
}

func (r *RPS) Play(ctx rps.JudgePlayContext, id rps.GameID) (rps.PlayResult, error) {
	names, _ := ctx.RemoteBlessings().ForContext(ctx)
	vlog.VI(1).Infof("Play %+v from %v", id, names)
	if len(names) == 0 {
		return rps.PlayResult{}, errors.New("no names provided for context")
	}
	return r.judge.play(ctx, names[0], id)
}

func (r *RPS) Challenge(ctx ipc.ServerContext, address string, id rps.GameID, opts rps.GameOptions) error {
	b, _ := ctx.RemoteBlessings().ForContext(ctx)
	vlog.VI(1).Infof("Challenge (%q, %+v, %+v) from %v", address, id, opts, b)
	newctx, _ := vtrace.SetNewTrace(r.ctx)
	return r.player.challenge(newctx, address, id, opts)
}

func (r *RPS) Record(ctx ipc.ServerContext, score rps.ScoreCard) error {
	b, _ := ctx.RemoteBlessings().ForContext(ctx)
	vlog.VI(1).Infof("Record (%+v) from %v", score, b)
	return r.scoreKeeper.Record(ctx, score)
}
