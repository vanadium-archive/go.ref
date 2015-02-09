// Command rpsplayer is a command-line implementation of the Player
// service that allows a human player to join the game.
package main

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"

	_ "v.io/core/veyron/profiles/roaming"
	sflag "v.io/core/veyron/security/flag"

	"v.io/apps/rps"
	"v.io/apps/rps/common"
)

var (
	name = flag.String("name", "", "identifier to publish itself as (defaults to user@hostname)")
)

func main() {
	rootctx, shutdown := veyron2.Init()
	defer shutdown()

	for {
		ctx, _ := vtrace.SetNewTrace(rootctx)
		if selectOne([]string{"Initiate Game", "Wait For Challenge"}) == 0 {
			initiateGame(ctx)
		} else {
			fmt.Println("Waiting to receive a challenge...")
			game := recvChallenge(ctx)
			playGame(ctx, game.address, game.id)
		}
		if selectOne([]string{"Play Again", "Quit"}) == 1 {
			break
		}
	}
}

type gameChallenge struct {
	address string
	id      rps.GameID
	opts    rps.GameOptions
}

// impl is a PlayerServerMethods implementation that prompts the user to accept
// or decline challenges. While waiting for a reply from the user, any incoming
// challenges are auto-declined.
type impl struct {
	ch      chan gameChallenge
	decline bool
	lock    sync.Mutex
}

func (i *impl) setDecline(v bool) bool {
	i.lock.Lock()
	defer i.lock.Unlock()
	prev := i.decline
	i.decline = v
	return prev
}

func (i *impl) Challenge(ctx ipc.ServerContext, address string, id rps.GameID, opts rps.GameOptions) error {
	remote, _ := ctx.RemoteBlessings().ForContext(ctx)
	vlog.VI(1).Infof("Challenge (%q, %+v) from %v", address, id, remote)
	// When setDecline(true) returns, future challenges will be declined.
	// Whether the current challenge should be considered depends on the
	// previous state. If 'decline' was already true, we need to decline
	// this challenge. It 'decline' was false, this is the first challenge
	// that we should process.
	if i.setDecline(true) {
		return errors.New("player is busy")
	}
	fmt.Println()
	fmt.Printf("Challenge received from %v for a %d-round ", remote, opts.NumRounds)
	switch opts.GameType {
	case rps.Classic:
		fmt.Print("Classic ")
	case rps.LizardSpock:
		fmt.Print("Lizard-Spock ")
	default:
	}
	fmt.Println("Game.")
	if selectOne([]string{"Accept", "Decline"}) == 0 {
		i.ch <- gameChallenge{address, id, opts}
		return nil
	}
	// Start considering challenges again.
	i.setDecline(false)
	return errors.New("player declined challenge")
}

// recvChallenge runs a server until a game challenge is accepted by the user.
// The server is stopped afterwards.
func recvChallenge(ctx *context.T) gameChallenge {
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	ch := make(chan gameChallenge)

	listenSpec := veyron2.GetListenSpec(ctx)
	ep, err := server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("Listen(%v) failed: %v", listenSpec, err)
	}
	if *name == "" {
		*name = common.CreateName()
	}
	if err := server.Serve(fmt.Sprintf("rps/player/%s", *name), rps.PlayerServer(&impl{ch: ch}), sflag.NewAuthorizerOrDie()); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	vlog.Infof("Listening on endpoint /%s", ep)
	result := <-ch
	server.Stop()
	return result
}

// initiateGame initiates a new game by getting a list of judges and players,
// and asking the user to select one of each, to select the game options, what
// to play, etc.
func initiateGame(ctx *context.T) error {
	jChan := make(chan []string)
	oChan := make(chan []string)
	go findAll(ctx, "judge", jChan)
	go findAll(ctx, "player", oChan)

	fmt.Println("Looking for available participants...")
	judges := <-jChan
	opponents := <-oChan
	fmt.Println()
	if len(judges) == 0 || len(opponents) == 0 {
		return errors.New("no one to play with")
	}

	fmt.Println("Choose a judge:")
	j := selectOne(judges)
	fmt.Println("Choose an opponent:")
	o := selectOne(opponents)
	fmt.Println("Choose the type of rock-paper-scissors game would you like to play:")
	gameType := selectOne([]string{"Classic", "LizardSpock"})
	fmt.Println("Choose the number of rounds required to win:")
	numRounds := selectOne([]string{"1", "2", "3", "4", "5", "6"}) + 1
	gameOpts := rps.GameOptions{NumRounds: int32(numRounds), GameType: rps.GameTypeTag(gameType)}

	gameID, err := createGame(ctx, judges[j], gameOpts)
	if err != nil {
		vlog.Infof("createGame: %v", err)
		return err
	}
	for {
		err := sendChallenge(ctx, opponents[o], judges[j], gameID, gameOpts)
		if err == nil {
			break
		}
		fmt.Printf("Challenge was declined by %s (%v)\n", opponents[o], err)
		fmt.Println("Choose another opponent:")
		o = selectOne(opponents)
	}
	fmt.Println("Joining the game...")

	if _, err = playGame(ctx, judges[j], gameID); err != nil {
		vlog.Infof("playGame: %v", err)
		return err
	}
	return nil
}

func createGame(ctx *context.T, server string, opts rps.GameOptions) (rps.GameID, error) {
	j := rps.RockPaperScissorsClient(server)
	return j.CreateGame(ctx, opts)
}

func sendChallenge(ctx *context.T, opponent, judge string, gameID rps.GameID, gameOpts rps.GameOptions) error {
	o := rps.RockPaperScissorsClient(opponent)
	return o.Challenge(ctx, judge, gameID, gameOpts)
}

func playGame(outer *context.T, judge string, gameID rps.GameID) (rps.PlayResult, error) {
	ctx, cancel := context.WithTimeout(outer, 10*time.Minute)
	defer cancel()
	fmt.Println()
	j := rps.RockPaperScissorsClient(judge)
	game, err := j.Play(ctx, gameID)
	if err != nil {
		return rps.PlayResult{}, err
	}
	var playerNum int32
	rStream := game.RecvStream()
	for rStream.Advance() {
		in := rStream.Value()
		if in.PlayerNum > 0 {
			playerNum = in.PlayerNum
			fmt.Printf("You are player %d\n", in.PlayerNum)
		}
		if len(in.OpponentName) > 0 {
			fmt.Printf("Your opponent is %q\n", in.OpponentName)
		}
		if len(in.RoundResult.Moves[0]) > 0 {
			if playerNum != 1 && playerNum != 2 {
				vlog.Fatalf("invalid playerNum: %d", playerNum)
			}
			fmt.Printf("You played %q\n", in.RoundResult.Moves[playerNum-1])
			fmt.Printf("Your opponent played %q\n", in.RoundResult.Moves[2-playerNum])
			if len(in.RoundResult.Comment) > 0 {
				fmt.Printf(">>> %s <<<\n", strings.ToUpper(in.RoundResult.Comment))
			}
			if in.RoundResult.Winner == 0 {
				fmt.Println("----- It's a draw -----")
			} else if rps.WinnerTag(playerNum) == in.RoundResult.Winner {
				fmt.Println("***** You WIN *****")
			} else {
				fmt.Println("##### You LOSE #####")
			}
		}
		if len(in.MoveOptions) > 0 {
			fmt.Println()
			fmt.Println("Choose your weapon:")
			m := selectOne(in.MoveOptions)
			if err := game.SendStream().Send(rps.PlayerAction{Move: in.MoveOptions[m]}); err != nil {
				return rps.PlayResult{}, err
			}
		}
		if len(in.Score.Players) > 0 {
			fmt.Println()
			fmt.Println("==== GAME SUMMARY ====")
			fmt.Print(common.FormatScoreCard(in.Score))
			fmt.Println("======================")
			if rps.WinnerTag(playerNum) == in.Score.Winner {
				fmt.Println("You won! :)")
			} else {
				fmt.Println("You lost! :(")
			}
		}
	}
	if err := rStream.Err(); err == nil {
		fmt.Println("Game Ended")
	} else {
		vlog.Infof("stream error: %v", err)
	}

	return game.Finish()
}

func selectOne(choices []string) (choice int) {
	if len(choices) == 0 {
		vlog.Fatal("No options to choose from!")
	}
	fmt.Println()
	for i, x := range choices {
		fmt.Printf("  %d. %s\n", i+1, x)
	}
	fmt.Println()
	for {
		if len(choices) == 1 {
			fmt.Print("Select one [1] --> ")
		} else {
			fmt.Printf("Select one [1-%d] --> ", len(choices))
		}
		fmt.Scanf("%d", &choice)
		if choice >= 1 && choice <= len(choices) {
			choice -= 1
			break
		}
	}
	fmt.Println()
	return
}

func findAll(ctx *context.T, t string, out chan []string) {
	ns := veyron2.GetNamespace(ctx)
	var result []string
	c, err := ns.Glob(ctx, "rps/"+t+"/*")
	if err != nil {
		vlog.Infof("ns.Glob failed: %v", err)
		out <- result
		return
	}
	for e := range c {
		if e.Error != nil {
			fmt.Print("E")
			continue
		}
		fmt.Print(".")
		result = append(result, e.Name)
	}
	if len(result) == 0 {
		vlog.Infof("found no %ss", t)
		out <- result
		return
	}
	sort.Strings(result)
	out <- result
}
