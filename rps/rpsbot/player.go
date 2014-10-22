package main

import (
	"math/rand"
	"time"

	"veyron.io/examples/rps"
	"veyron.io/examples/rps/common"
	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/lib/stats/counter"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/vlog"
)

type Player struct {
	gamesPlayed     *counter.Counter
	gamesWon        *counter.Counter
	gamesInProgress *stats.Integer
}

func NewPlayer() *Player {
	return &Player{
		gamesPlayed:     stats.NewCounter("player/games-played"),
		gamesWon:        stats.NewCounter("player/games-won"),
		gamesInProgress: stats.NewInteger("player/games-in-progress"),
	}
}

func (p *Player) Stats() (played, won int64) {
	played = p.gamesPlayed.Value()
	won = p.gamesWon.Value()
	return
}

// only used by tests.
func (p *Player) WaitUntilIdle() {
	for p.gamesInProgress.Value() != int64(0) {
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *Player) InitiateGame(ctx context.T) error {
	judge, err := common.FindJudge(ctx)
	if err != nil {
		vlog.Infof("FindJudge: %v", err)
		return err
	}
	gameID, gameOpts, err := p.createGame(ctx, judge)
	if err != nil {
		vlog.Infof("createGame: %v", err)
		return err
	}
	vlog.VI(1).Infof("Created gameID %q on %q", gameID, judge)

	for {
		opponent, err := common.FindPlayer(ctx)
		if err != nil {
			vlog.Infof("FindPlayer: %v", err)
			return err
		}
		vlog.VI(1).Infof("chosen opponent is %q", opponent)
		if err = p.sendChallenge(ctx, opponent, judge, gameID, gameOpts); err == nil {
			break
		}
		vlog.Infof("sendChallenge: %v", err)
	}
	result, err := p.playGame(ctx, judge, gameID)
	if err != nil {
		vlog.Infof("playGame: %v", err)
		return err
	}
	if result.YouWon {
		vlog.VI(1).Info("Game result: I won! :)")
	} else {
		vlog.VI(1).Info("Game result: I lost :(")
	}
	return nil
}

func (p *Player) createGame(ctx context.T, server string) (rps.GameID, rps.GameOptions, error) {
	j, err := rps.BindRockPaperScissors(server)
	if err != nil {
		return rps.GameID{}, rps.GameOptions{}, err
	}
	numRounds := 3 + rand.Intn(3)
	gameType := rps.Classic
	if rand.Intn(2) == 1 {
		gameType = rps.LizardSpock
	}
	gameOpts := rps.GameOptions{NumRounds: int32(numRounds), GameType: gameType}
	gameId, err := j.CreateGame(ctx, gameOpts)
	return gameId, gameOpts, err
}

func (p *Player) sendChallenge(ctx context.T, opponent, judge string, gameID rps.GameID, gameOpts rps.GameOptions) error {
	o, err := rps.BindRockPaperScissors(opponent)
	if err != nil {
		return err
	}
	return o.Challenge(ctx, judge, gameID, gameOpts)
}

// challenge receives an incoming challenge and starts to play a new game.
// Note that the new game will occur in a new context.
func (p *Player) challenge(rt veyron2.Runtime, judge string, gameID rps.GameID, _ rps.GameOptions) error {
	vlog.VI(1).Infof("challenge received: %s %v", judge, gameID)
	go p.playGame(rt.NewContext(), judge, gameID)
	return nil
}

// playGame plays an entire game, which really only consists of reading
// commands from the server, and picking a random "move" when asked to.
func (p *Player) playGame(outer context.T, judge string, gameID rps.GameID) (rps.PlayResult, error) {
	ctx, cancel := outer.WithTimeout(10 * time.Minute)
	defer cancel()
	p.gamesInProgress.Incr(1)
	defer p.gamesInProgress.Incr(-1)
	j, err := rps.BindRockPaperScissors(judge)
	if err != nil {
		return rps.PlayResult{}, err
	}
	game, err := j.Play(ctx, gameID)
	if err != nil {
		return rps.PlayResult{}, err
	}
	rStream := game.RecvStream()
	sender := game.SendStream()
	for rStream.Advance() {
		in := rStream.Value()
		if in.PlayerNum > 0 {
			vlog.VI(1).Infof("I'm player %d", in.PlayerNum)
		}
		if len(in.OpponentName) > 0 {
			vlog.VI(1).Infof("My opponent is %q", in.OpponentName)
		}
		if len(in.MoveOptions) > 0 {
			n := rand.Intn(len(in.MoveOptions))
			vlog.VI(1).Infof("My turn to play. Picked %q from %v", in.MoveOptions[n], in.MoveOptions)
			if err := sender.Send(rps.PlayerAction{Move: in.MoveOptions[n]}); err != nil {
				return rps.PlayResult{}, err
			}
		}
		if len(in.RoundResult.Moves[0]) > 0 {
			vlog.VI(1).Infof("Player 1 played %q. Player 2 played %q. Winner: %v %s",
				in.RoundResult.Moves[0], in.RoundResult.Moves[1], in.RoundResult.Winner, in.RoundResult.Comment)
		}
		if len(in.Score.Players) > 0 {
			vlog.VI(1).Infof("Score card: %s", common.FormatScoreCard(in.Score))
		}
	}

	if err := rStream.Err(); err != nil {
		vlog.Infof("stream error: %v", err)
	} else {
		vlog.VI(1).Infof("Game Ended")
	}
	result, err := game.Finish()
	p.gamesPlayed.Incr(1)
	if err == nil && result.YouWon {
		p.gamesWon.Incr(1)
	}
	return result, err
}
