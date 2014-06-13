package impl

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	rps "veyron/examples/rockpaperscissors"
	"veyron/examples/rockpaperscissors/common"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	errBadGameID      = errors.New("requested gameID doesn't exist")
	errTooManyPlayers = errors.New("all players are already seated")
)

type Judge struct {
	mt       naming.MountTable
	lock     sync.Mutex
	games    map[rps.GameID]*gameInfo
	gamesRun common.Counter
}

type gameInfo struct {
	id         rps.GameID
	startTime  time.Time
	score      rps.ScoreCard
	streams    []rps.JudgeServicePlayStream
	playerChan chan playerInput
	scoreChan  chan scoreData
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

func NewJudge(mt naming.MountTable) *Judge {
	return &Judge{mt: mt, games: make(map[rps.GameID]*gameInfo)}
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
		id:         id,
		startTime:  now,
		score:      score,
		playerChan: make(chan playerInput),
		scoreChan:  make(chan scoreData),
	}
	return id, nil
}

// play interacts with a player for the duration of a game.
func (j *Judge) play(name string, id rps.GameID, stream rps.JudgeServicePlayStream) (rps.PlayResult, error) {
	vlog.VI(1).Infof("play from %q for %v", name, id)
	nilResult := rps.PlayResult{}

	c, s, err := j.gameChannels(id)
	if err != nil {
		return nilResult, err
	}
	playerNum, err := j.addPlayer(name, id, stream)
	if err != nil {
		return nilResult, err
	}
	vlog.VI(1).Infof("This is player %d", playerNum)

	// Send all user input to the player channel.
	done := make(chan struct{}, 1)
	defer func() { done <- struct{}{} }()
	go func() {
		for {
			action, err := stream.Recv()
			if err != nil {
				select {
				case c <- playerInput{player: playerNum, action: rps.PlayerAction{Quit: true}}:
				case <-done:
				}
				return
			}
			select {
			case c <- playerInput{player: playerNum, action: action}:
			case <-done:
				return
			}
		}
	}()

	if err := stream.Send(rps.JudgeAction{PlayerNum: int32(playerNum)}); err != nil {
		return nilResult, err
	}
	// When the second player connects, we start the game.
	if playerNum == 2 {
		go j.manageGame(id)
	}
	// Wait for the ScoreCard.
	scoreData := <-s
	if scoreData.err != nil {
		return rps.PlayResult{}, scoreData.err
	}
	return rps.PlayResult{YouWon: scoreData.score.Winner == rps.WinnerTag(playerNum)}, nil
}

func (j *Judge) manageGame(id rps.GameID) {
	j.gamesRun.Add(1)
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
		action := rps.JudgeAction{OpponentName: info.score.Players[opp]}
		if err := info.streams[p].Send(action); err != nil {
			err := scoreData{err: err}
			info.scoreChan <- err
			info.scoreChan <- err
			return
		}
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
	for _, s := range info.streams {
		if err := s.Send(action); err != nil {
			vlog.Infof("Stream closed after game finished")
		}
	}

	// Send the score card to the score keepers.
	keepers, err := common.FindScoreKeepers(j.mt)
	if err != nil || len(keepers) == 0 {
		vlog.Infof("No score keepers: %v", err)
		return
	}
	done := make(chan bool)
	for _, k := range keepers {
		go j.sendScore(k, info.score, done)
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
	for _, s := range info.streams {
		if err := s.Send(action); err != nil {
			return round, err
		}
	}
	for x := 0; x < 2; x++ {
		in := <-info.playerChan
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
	round.Winner = j.compareMoves(round.Moves[0], round.Moves[1])
	vlog.VI(1).Infof("Player 1 played %q. Player 2 played %q. Winner: %d", round.Moves[0], round.Moves[1], round.Winner)

	action = rps.JudgeAction{RoundResult: round}
	for _, s := range info.streams {
		if err := s.Send(action); err != nil {
			return round, err
		}
	}
	round.EndTimeNS = time.Now().UnixNano()
	return round, nil
}

func (j *Judge) addPlayer(name string, id rps.GameID, stream rps.JudgeServicePlayStream) (int, error) {
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
	info.streams = append(info.streams, stream)
	return len(info.streams), nil
}

func (j *Judge) gameChannels(id rps.GameID) (chan playerInput, chan scoreData, error) {
	j.lock.Lock()
	defer j.lock.Unlock()
	info, exists := j.games[id]
	if !exists {
		return nil, nil, errBadGameID
	}
	return info.playerChan, info.scoreChan, nil
}

func (j *Judge) sendScore(address string, score rps.ScoreCard, done chan bool) error {
	defer func() { done <- true }()
	k, err := rps.BindRockPaperScissors(address)
	if err != nil {
		vlog.Infof("BindRockPaperScissors: %v", err)
		return err
	}
	err = k.Record(rt.R().TODOContext(), score)
	if err != nil {
		vlog.Infof("Record: %v", err)
		return err
	}
	return nil
}

func (j *Judge) compareMoves(m1, m2 string) rps.WinnerTag {
	if m1 == m2 {
		return rps.Draw
	}
	if m1 == "rock" && (m2 == "scissors" || m2 == "lizard") {
		return rps.Player1
	}
	if m1 == "paper" && (m2 == "rock" || m2 == "spock") {
		return rps.Player1
	}
	if m1 == "scissors" && (m2 == "paper" || m2 == "lizard") {
		return rps.Player1
	}
	if m1 == "lizard" && (m2 == "paper" || m2 == "spock") {
		return rps.Player1
	}
	if m1 == "spock" && (m2 == "scissors" || m2 == "rock") {
		return rps.Player1
	}
	return rps.Player2
}
