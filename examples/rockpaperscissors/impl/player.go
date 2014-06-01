package impl

import (
	"io"
	"math/rand"
	"sync"
	"time"

	rps "veyron/examples/rockpaperscissors"
	"veyron/examples/rockpaperscissors/common"

	"veyron2"
	"veyron2/naming"
	"veyron2/vlog"
)

type Player struct {
	mt          naming.MountTable
	lock        sync.Mutex
	gamesPlayed common.Counter
	gamesWon    common.Counter
}

func NewPlayer(mt naming.MountTable) *Player {
	return &Player{mt: mt}
}

func (p *Player) Stats() (played, won int64) {
	played = p.gamesPlayed.Value()
	won = p.gamesWon.Value()
	return
}

func (p *Player) InitiateGame() error {
	judge, err := common.FindJudge(p.mt)
	if err != nil {
		vlog.Infof("FindJudge: %v", err)
		return err
	}
	gameID, err := p.createGame(judge)
	if err != nil {
		vlog.Infof("createGame: %v", err)
		return err
	}
	vlog.Infof("Created gameID %q on %q", gameID, judge)

	opponent, err := common.FindPlayer(p.mt)
	if err != nil {
		vlog.Infof("FindPlayer: %v", err)
		return err
	}
	vlog.Infof("chosen opponent is %q", opponent)
	if err = p.sendChallenge(opponent, judge, gameID); err != nil {
		vlog.Infof("sendChallenge: %v", err)
		return err
	}
	result, err := p.playGame(judge, gameID)
	if err != nil {
		vlog.Infof("playGame: %v", err)
		return err
	}
	if result.YouWon {
		vlog.Info("Game result: I won! :)")
	} else {
		vlog.Info("Game result: I lost :(")
	}
	return nil
}

func (p *Player) createGame(server string) (rps.GameID, error) {
	j, err := rps.BindRockPaperScissors(server)
	if err != nil {
		return rps.GameID{}, err
	}
	numRounds := 3 + rand.Intn(3)
	gameType := rps.Classic
	if rand.Intn(2) == 1 {
		gameType = rps.LizardSpock
	}
	return j.CreateGame(rps.GameOptions{NumRounds: int32(numRounds), GameType: gameType})
}

func (p *Player) sendChallenge(opponent, judge string, gameID rps.GameID) error {
	o, err := rps.BindRockPaperScissors(opponent)
	if err != nil {
		return err
	}
	return o.Challenge(judge, gameID)
}

// challenge receives an incoming challenge.
func (p *Player) challenge(judge string, gameID rps.GameID) error {
	vlog.Infof("challenge received: %s %v", judge, gameID)
	go p.playGame(judge, gameID)
	return nil
}

// playGame plays an entire game, which really only consists of reading
// commands from the server, and picking a random "move" when asked to.
func (p *Player) playGame(judge string, gameID rps.GameID) (rps.PlayResult, error) {
	j, err := rps.BindRockPaperScissors(judge)
	if err != nil {
		return rps.PlayResult{}, err
	}
	game, err := j.Play(gameID, veyron2.CallTimeout(10*time.Minute))
	if err != nil {
		return rps.PlayResult{}, err
	}
	for {
		in, err := game.Recv()
		if err == io.EOF {
			vlog.Infof("Game Ended")
			break
		}
		if err != nil {
			vlog.Infof("recv error: %v", err)
			break
		}
		if in.PlayerNum > 0 {
			vlog.Infof("I'm player %d", in.PlayerNum)
		}
		if len(in.OpponentName) > 0 {
			vlog.Infof("My opponent is %q", in.OpponentName)
		}
		if len(in.MoveOptions) > 0 {
			n := rand.Intn(len(in.MoveOptions))
			vlog.Infof("My turn to play. Picked %q from %v", in.MoveOptions[n], in.MoveOptions)
			if err := game.Send(rps.PlayerAction{Move: in.MoveOptions[n]}); err != nil {
				return rps.PlayResult{}, err
			}
		}
		if len(in.RoundResult.Moves[0]) > 0 {
			vlog.Infof("Player 1 played %q. Player 2 played %q. Winner: %v",
				in.RoundResult.Moves[0], in.RoundResult.Moves[1], in.RoundResult.Winner)
		}
		if len(in.Score.Players) > 0 {
			vlog.Infof("Score card: %s", common.FormatScoreCard(in.Score))
		}
	}
	result, err := game.Finish()
	p.gamesPlayed.Add(1)
	if err == nil && result.YouWon {
		p.gamesWon.Add(1)
	}
	return result, err
}
