package main

import (
	"math/rand"
	"time"

	"v.io/apps/rps"
	"v.io/apps/rps/common"
	"v.io/core/veyron/lib/stats"
	"v.io/core/veyron/lib/stats/counter"
	"v.io/v23/context"
	"v.io/v23/vlog"
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

func (p *Player) InitiateGame(ctx *context.T) error {
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

func (p *Player) createGame(ctx *context.T, server string) (rps.GameID, rps.GameOptions, error) {
	j := rps.RockPaperScissorsClient(server)
	numRounds := 3 + rand.Intn(3)
	gameType := rps.Classic
	if rand.Intn(2) == 1 {
		gameType = rps.LizardSpock
	}
	gameOpts := rps.GameOptions{NumRounds: int32(numRounds), GameType: gameType}
	gameId, err := j.CreateGame(ctx, gameOpts)
	return gameId, gameOpts, err
}

func (p *Player) sendChallenge(ctx *context.T, opponent, judge string, gameID rps.GameID, gameOpts rps.GameOptions) error {
	o := rps.RockPaperScissorsClient(opponent)
	return o.Challenge(ctx, judge, gameID, gameOpts)
}

// challenge receives an incoming challenge and starts to play a new game.
// Note that the new game will occur in a new context.
func (p *Player) challenge(ctx *context.T, judge string, gameID rps.GameID, _ rps.GameOptions) error {
	vlog.VI(1).Infof("challenge received: %s %v", judge, gameID)
	go p.playGame(ctx, judge, gameID)
	return nil
}

// playGame plays an entire game, which really only consists of reading
// commands from the server, and picking a random "move" when asked to.
func (p *Player) playGame(outer *context.T, judge string, gameID rps.GameID) (rps.PlayResult, error) {
	ctx, cancel := context.WithTimeout(outer, 10*time.Minute)
	defer cancel()
	p.gamesInProgress.Incr(1)
	defer p.gamesInProgress.Incr(-1)
	j := rps.RockPaperScissorsClient(judge)
	game, err := j.Play(ctx, gameID)
	if err != nil {
		return rps.PlayResult{}, err
	}
	rStream := game.RecvStream()
	sender := game.SendStream()
	for rStream.Advance() {
		in := rStream.Value()
		switch v := in.(type) {
		case rps.JudgeActionPlayerNum:
			vlog.VI(1).Infof("I'm player %d", v.Value)
		case rps.JudgeActionOpponentName:
			vlog.VI(1).Infof("My opponent is %q", v.Value)
		case rps.JudgeActionMoveOptions:
			opts := v.Value
			n := rand.Intn(len(opts))
			vlog.VI(1).Infof("My turn to play. Picked %q from %v", opts[n], opts)
			if err := sender.Send(rps.PlayerActionMove{opts[n]}); err != nil {
				return rps.PlayResult{}, err
			}
		case rps.JudgeActionRoundResult:
			rr := v.Value
			vlog.VI(1).Infof("Player 1 played %q. Player 2 played %q. Winner: %v %s",
				rr.Moves[0], rr.Moves[1], rr.Winner, rr.Comment)
		case rps.JudgeActionScore:
			vlog.VI(1).Infof("Score card: %s", common.FormatScoreCard(v.Value))
		default:
			vlog.Infof("unexpected message type: %T", in)
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
