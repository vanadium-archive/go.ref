package impl

import (
	"errors"

	rps "veyron/examples/rockpaperscissors"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/vlog"
)

// RPS implements rockpaperscissors.RockPaperScissorsService
type RPS struct {
	mt          naming.MountTable
	player      *Player
	judge       *Judge
	scoreKeeper *ScoreKeeper
}

func NewRPS(mt naming.MountTable) *RPS {
	return &RPS{mt: mt, player: NewPlayer(mt), judge: NewJudge(mt), scoreKeeper: NewScoreKeeper()}
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

func (r *RPS) CreateGame(ctx ipc.Context, opts rps.GameOptions) (rps.GameID, error) {
	vlog.VI(1).Infof("CreateGame %+v from %s", opts, ctx.RemoteID())
	names := ctx.LocalID().Names()
	if len(names) == 0 {
		return rps.GameID{}, errors.New("no names provided with local ID")
	}
	return r.judge.createGame(names[0], opts)
}

func (r *RPS) Play(ctx ipc.Context, id rps.GameID, stream rps.JudgeServicePlayStream) (rps.PlayResult, error) {
	vlog.VI(1).Infof("Play %+v from %s", id, ctx.RemoteID())
	names := ctx.RemoteID().Names()
	if len(names) == 0 {
		return rps.PlayResult{}, errors.New("no names provided with remote ID")
	}
	return r.judge.play(names[0], id, stream)
}

func (r *RPS) Challenge(ctx ipc.Context, address string, id rps.GameID) error {
	vlog.VI(1).Infof("Challenge (%q, %+v) from %s", address, id, ctx.RemoteID())
	return r.player.challenge(address, id)
}

func (r *RPS) Record(ctx ipc.Context, score rps.ScoreCard) error {
	vlog.VI(1).Infof("Record (%+v) from %s", score, ctx.RemoteID())
	return r.scoreKeeper.Record(ctx, score)
}
