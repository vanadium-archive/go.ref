package main

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"veyron.io/apps/rps"
	"veyron.io/apps/rps/common"
	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/lib/stats/counter"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/vlog"
)

var (
	errBadGameID      = errors.New("requested gameID doesn't exist")
	errTooManyPlayers = errors.New("all players are already seated")
)

type Judge struct {
	lock     sync.Mutex
	games    map[rps.GameID]*gameInfo
	gamesRun *counter.Counter
}

type sendStream interface {
	Send(item rps.JudgeAction) error
}

type gameInfo struct {
	id        rps.GameID
	startTime time.Time
	score     rps.ScoreCard
	streams   []sendStream
	playerIn  chan playerInput
	playerOut []chan rps.JudgeAction
	scoreChan chan scoreData
}

func (g *gameInfo) moveOptions() []string {
	switch g.score.Opts.GameType {
	case rps.LizardSpock:
		return []string{"rock", "paper", "scissors", "lizard", "spock"}
	default:
		return []string{"rock", "paper", "scissors"}
	}
}

func (g *gameInfo) validMove(m string) bool {
	for _, x := range g.moveOptions() {
		if x == m {
			return true
		}
	}
	return false
}

type playerInput struct {
	player int
	action rps.PlayerAction
}

type scoreData struct {
	err   error
	score rps.ScoreCard
}

func NewJudge() *Judge {
	return &Judge{
		games:    make(map[rps.GameID]*gameInfo),
		gamesRun: stats.NewCounter("judge/games-run"),
	}
}

func (j *Judge) Stats() int64 {
	return j.gamesRun.Value()
}

// createGame handles a request to create a new game.
func (j *Judge) createGame(ownName string, opts rps.GameOptions) (rps.GameID, error) {
	vlog.VI(1).Infof("createGame called")
	score := rps.ScoreCard{Opts: opts, Judge: ownName}
	now := time.Now()
	id := rps.GameID{ID: strconv.FormatInt(now.UnixNano(), 16)}

	j.lock.Lock()
	defer j.lock.Unlock()
	for k, v := range j.games {
		if now.Sub(v.startTime) > 1*time.Hour {
			vlog.Infof("Removing stale game ID %v", k)
			delete(j.games, k)
		}
	}
	j.games[id] = &gameInfo{
		id:        id,
		startTime: now,
		score:     score,
		playerIn:  make(chan playerInput),
		playerOut: []chan rps.JudgeAction{
			make(chan rps.JudgeAction),
			make(chan rps.JudgeAction),
		},
		scoreChan: make(chan scoreData),
	}
	return id, nil
}

// play interacts with a player for the duration of a game.
func (j *Judge) play(ctx rps.JudgePlayContext, name string, id rps.GameID) (rps.PlayResult, error) {
	vlog.VI(1).Infof("play from %q for %v", name, id)
	nilResult := rps.PlayResult{}

	pIn, pOut, s, err := j.gameChannels(id)
	if err != nil {
		return nilResult, err
	}
	playerNum, err := j.addPlayer(name, id, ctx)
	if err != nil {
		return nilResult, err
	}
	vlog.VI(1).Infof("This is player %d", playerNum)

	// Send all user input to the player input channel.
	done := make(chan struct{})
	defer close(done)
	go func() {
		rStream := ctx.RecvStream()
		for rStream.Advance() {
			action := rStream.Value()
			select {
			case pIn <- playerInput{player: playerNum, action: action}:
			case <-done:
				return
			}
		}
		select {
		case pIn <- playerInput{player: playerNum, action: rps.PlayerAction{Quit: true}}:
		case <-done:
		}
	}()
	// Send all the output to the user.
	go func() {
		for packet := range pOut[playerNum-1] {
			if err := ctx.SendStream().Send(packet); err != nil {
				vlog.Infof("error sending to player stream: %v", err)
			}
		}
	}()
	defer close(pOut[playerNum-1])

	pOut[playerNum-1] <- rps.JudgeAction{PlayerNum: int32(playerNum)}

	// When the second player connects, we start the game.
	if playerNum == 2 {
		go j.manageGame(ctx, id)
	}
	// Wait for the ScoreCard.
	scoreData := <-s
	if scoreData.err != nil {
		return rps.PlayResult{}, scoreData.err
	}
	return rps.PlayResult{YouWon: scoreData.score.Winner == rps.WinnerTag(playerNum)}, nil
}

func (j *Judge) manageGame(ctx context.T, id rps.GameID) {
	j.gamesRun.Incr(1)
	j.lock.Lock()
	info, exists := j.games[id]
	if !exists {
		e := scoreData{err: errBadGameID}
		info.scoreChan <- e
		info.scoreChan <- e
		return
	}
	delete(j.games, id)
	j.lock.Unlock()

	// Inform each player of their opponent's name.
	for p := 0; p < 2; p++ {
		opp := 1 - p
		info.playerOut[p] <- rps.JudgeAction{OpponentName: info.score.Players[opp]}
	}

	win1, win2 := 0, 0
	goal := int(info.score.Opts.NumRounds)
	// Play until one player has won 'goal' times.
	for win1 < goal && win2 < goal {
		round, err := j.playOneRound(info)
		if err != nil {
			err := scoreData{err: err}
			info.scoreChan <- err
			info.scoreChan <- err
			return
		}
		if round.Winner == rps.Player1 {
			win1++
		} else if round.Winner == rps.Player2 {
			win2++
		}
		info.score.Rounds = append(info.score.Rounds, round)
	}
	if win1 > win2 {
		info.score.Winner = rps.Player1
	} else {
		info.score.Winner = rps.Player2
	}

	info.score.StartTimeNS = info.startTime.UnixNano()
	info.score.EndTimeNS = time.Now().UnixNano()

	// Send the score card to the players.
	action := rps.JudgeAction{Score: info.score}
	for _, p := range info.playerOut {
		p <- action
	}

	// Send the score card to the score keepers.
	keepers, err := common.FindScoreKeepers(ctx)
	if err != nil || len(keepers) == 0 {
		vlog.Infof("No score keepers: %v", err)
		return
	}
	done := make(chan bool)
	for _, k := range keepers {
		go j.sendScore(ctx, k, info.score, done)
	}
	for _ = range keepers {
		<-done
	}

	info.scoreChan <- scoreData{score: info.score}
	info.scoreChan <- scoreData{score: info.score}
}

func (j *Judge) playOneRound(info *gameInfo) (rps.Round, error) {
	round := rps.Round{StartTimeNS: time.Now().UnixNano()}
	action := rps.JudgeAction{MoveOptions: info.moveOptions()}
	for _, p := range info.playerOut {
		p <- action
	}
	for x := 0; x < 2; x++ {
		in := <-info.playerIn
		if in.action.Quit {
			return round, fmt.Errorf("player %d quit the game", in.player)
		}
		if !info.validMove(in.action.Move) {
			return round, fmt.Errorf("player %d made an invalid move: %s", in.player, in.action.Move)
		}
		if len(round.Moves[in.player-1]) > 0 {
			return round, fmt.Errorf("player %d played twice in the same round!", in.player)
		}
		round.Moves[in.player-1] = in.action.Move
	}
	round.Winner, round.Comment = j.compareMoves(round.Moves[0], round.Moves[1])
	vlog.VI(1).Infof("Player 1 played %q. Player 2 played %q. Winner: %d %s", round.Moves[0], round.Moves[1], round.Winner, round.Comment)

	action = rps.JudgeAction{RoundResult: round}
	for _, p := range info.playerOut {
		p <- action
	}
	round.EndTimeNS = time.Now().UnixNano()
	return round, nil
}

func (j *Judge) addPlayer(name string, id rps.GameID, stream rps.JudgePlayServerStream) (int, error) {
	j.lock.Lock()
	defer j.lock.Unlock()

	info, exists := j.games[id]
	if !exists {
		return 0, errBadGameID
	}
	if len(info.streams) == 2 {
		return 0, errTooManyPlayers
	}
	info.score.Players = append(info.score.Players, name)
	info.streams = append(info.streams, stream.SendStream())
	return len(info.streams), nil
}

func (j *Judge) gameChannels(id rps.GameID) (chan playerInput, []chan rps.JudgeAction, chan scoreData, error) {
	j.lock.Lock()
	defer j.lock.Unlock()
	info, exists := j.games[id]
	if !exists {
		return nil, nil, nil, errBadGameID
	}
	return info.playerIn, info.playerOut, info.scoreChan, nil
}

func (j *Judge) sendScore(ctx context.T, address string, score rps.ScoreCard, done chan bool) error {
	defer func() { done <- true }()
	k := rps.RockPaperScissorsClient(address)
	if err := k.Record(ctx, score); err != nil {
		vlog.Infof("Record: %v", err)
		return err
	}
	return nil
}

var moveComments = map[string]string{
	"lizard-paper":    "lizard eats paper",
	"lizard-rock":     "rock crushes lizard",
	"lizard-scissors": "scissors decapitates lizard",
	"lizard-spock":    "lizard poisons spock",
	"paper-rock":      "paper covers rock",
	"paper-scissors":  "scissors cuts paper",
	"paper-spock":     "paper disproves spock",
	"rock-scissors":   "rock crushes scissors",
	"rock-spock":      "spock vaporizes rock",
	"scissors-spock":  "spock smashes scissors",
}

func (j *Judge) compareMoves(m1, m2 string) (winner rps.WinnerTag, comment string) {
	if m1 < m2 {
		comment = moveComments[m1+"-"+m2]
	} else {
		comment = moveComments[m2+"-"+m1]
	}
	if m1 == m2 {
		winner = rps.Draw
		return
	}
	if m1 == "rock" && (m2 == "scissors" || m2 == "lizard") {
		winner = rps.Player1
		return
	}
	if m1 == "paper" && (m2 == "rock" || m2 == "spock") {
		winner = rps.Player1
		return
	}
	if m1 == "scissors" && (m2 == "paper" || m2 == "lizard") {
		winner = rps.Player1
		return
	}
	if m1 == "lizard" && (m2 == "paper" || m2 == "spock") {
		winner = rps.Player1
		return
	}
	if m1 == "spock" && (m2 == "scissors" || m2 == "rock") {
		winner = rps.Player1
		return
	}
	winner = rps.Player2
	return
}
