// This file was auto-generated by the veyron vdl tool.
// Source: service.vdl

// Package rps is an example of veyron service for playing the game of
// Rock-Paper-Scissors. (http://en.wikipedia.org/wiki/Rock-paper-scissors)
//
// There are three different roles in the game:
//
// 1. Judge: A judge enforces the rules of the game and decides who
//    the winner is. At the end of the game, the judge reports the
//    final score to all the score keepers.
//
// 2. Player: A player can ask a judge to start a new game, it can
//    challenge another player, and it can play a game.
//
// 3. ScoreKeeper: A score keeper receives the final score for a game
//    after it ended.
package rps

import (
	"veyron.io/veyron/veyron2/services/security/access"

	// The non-user imports are prefixed with "__" to prevent collisions.
	__io "io"
	__veyron2 "veyron.io/veyron/veyron2"
	__context "veyron.io/veyron/veyron2/context"
	__ipc "veyron.io/veyron/veyron2/ipc"
	__vdlutil "veyron.io/veyron/veyron2/vdl/vdlutil"
	__wiretype "veyron.io/veyron/veyron2/wiretype"
)

// TODO(toddw): Remove this line once the new signature support is done.
// It corrects a bug where __wiretype is unused in VDL pacakges where only
// bootstrap types are used on interfaces.
const _ = __wiretype.TypeIDInvalid

// A GameID is used to uniquely identify a game within one Judge.
type GameID struct {
	ID string
}

// GameOptions specifies the parameters of a game.
type GameOptions struct {
	NumRounds int32       // The number of rounds that a player must win to win the game.
	GameType  GameTypeTag // The type of game to play: Classic or LizardSpock.
}

type GameTypeTag byte

type PlayerAction struct {
	Move string // The move that the player wants to make.
	Quit bool   // Whether the player wants to quit the game.
}

type JudgeAction struct {
	PlayerNum    int32     // The player's number.
	OpponentName string    // The name of the opponent.
	MoveOptions  []string  // A list of allowed moves that the player must choose from. Not always present.
	RoundResult  Round     // The result of the previous round. Not always present.
	Score        ScoreCard // The result of the game. Not always present.
}

// Round represents the state of a round.
type Round struct {
	Moves       [2]string // Each player's move.
	Comment     string    // A text comment from judge about the round.
	Winner      WinnerTag // Who won the round.
	StartTimeNS int64     // The time at which the round started.
	EndTimeNS   int64     // The time at which the round ended.
}

// WinnerTag is a type used to indicate whether a round or a game was a draw,
// was won by player 1 or was won by player 2.
type WinnerTag byte

// PlayResult is the value returned by the Play method. It indicates the outcome of the game.
type PlayResult struct {
	YouWon bool // True if the player receiving the result won the game.
}

type ScoreCard struct {
	Opts        GameOptions // The game options.
	Judge       string      // The name of the judge.
	Players     []string    // The name of the players.
	Rounds      []Round     // The outcome of each round.
	StartTimeNS int64       // The time at which the game started.
	EndTimeNS   int64       // The time at which the game ended.
	Winner      WinnerTag   // Who won the game.
}

const Classic = GameTypeTag(0) // Rock-Paper-Scissors

const LizardSpock = GameTypeTag(1) // Rock-Paper-Scissors-Lizard-Spock

const Draw = WinnerTag(0)

const Player1 = WinnerTag(1)

const Player2 = WinnerTag(2)

// JudgeClientMethods is the client interface
// containing Judge methods.
type JudgeClientMethods interface {
	// CreateGame creates a new game with the given game options and returns a game
	// identifier that can be used by the players to join the game.
	CreateGame(ctx __context.T, Opts GameOptions, opts ...__ipc.CallOpt) (GameID, error)
	// Play lets a player join an existing game and play.
	Play(ctx __context.T, ID GameID, opts ...__ipc.CallOpt) (JudgePlayCall, error)
}

// JudgeClientStub adds universal methods to JudgeClientMethods.
type JudgeClientStub interface {
	JudgeClientMethods
	__ipc.UniversalServiceMethods
}

// JudgeClient returns a client stub for Judge.
func JudgeClient(name string, opts ...__ipc.BindOpt) JudgeClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implJudgeClientStub{name, client}
}

type implJudgeClientStub struct {
	name   string
	client __ipc.Client
}

func (c implJudgeClientStub) c(ctx __context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.RuntimeFromContext(ctx).Client()
}

func (c implJudgeClientStub) CreateGame(ctx __context.T, i0 GameOptions, opts ...__ipc.CallOpt) (o0 GameID, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "CreateGame", []interface{}{i0}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

func (c implJudgeClientStub) Play(ctx __context.T, i0 GameID, opts ...__ipc.CallOpt) (ocall JudgePlayCall, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Play", []interface{}{i0}, opts...); err != nil {
		return
	}
	ocall = &implJudgePlayCall{Call: call}
	return
}

func (c implJudgeClientStub) Signature(ctx __context.T, opts ...__ipc.CallOpt) (o0 __ipc.ServiceSignature, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

// JudgePlayClientStream is the client stream for Judge.Play.
type JudgePlayClientStream interface {
	// RecvStream returns the receiver side of the Judge.Play client stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() JudgeAction
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
	// SendStream returns the send side of the Judge.Play client stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending, or if Send is called after Close or Cancel.  Blocks if
		// there is no buffer space; will unblock when buffer space is available or
		// after Cancel.
		Send(item PlayerAction) error
		// Close indicates to the server that no more items will be sent; server
		// Recv calls will receive io.EOF after all sent items.  This is an optional
		// call - e.g. a client might call Close if it needs to continue receiving
		// items from the server after it's done sending.  Returns errors
		// encountered while closing, or if Close is called after Cancel.  Like
		// Send, blocks if there is no buffer space available.
		Close() error
	}
}

// JudgePlayCall represents the call returned from Judge.Play.
type JudgePlayCall interface {
	JudgePlayClientStream
	// Finish performs the equivalent of SendStream().Close, then blocks until
	// the server is done, and returns the positional return values for the call.
	//
	// Finish returns immediately if Cancel has been called; depending on the
	// timing the output could either be an error signaling cancelation, or the
	// valid positional return values from the server.
	//
	// Calling Finish is mandatory for releasing stream resources, unless Cancel
	// has been called or any of the other methods return an error.  Finish should
	// be called at most once.
	Finish() (PlayResult, error)
	// Cancel cancels the RPC, notifying the server to stop processing.  It is
	// safe to call Cancel concurrently with any of the other stream methods.
	// Calling Cancel after Finish has returned is a no-op.
	Cancel()
}

type implJudgePlayCall struct {
	__ipc.Call
	valRecv JudgeAction
	errRecv error
}

func (c *implJudgePlayCall) RecvStream() interface {
	Advance() bool
	Value() JudgeAction
	Err() error
} {
	return implJudgePlayCallRecv{c}
}

type implJudgePlayCallRecv struct {
	c *implJudgePlayCall
}

func (c implJudgePlayCallRecv) Advance() bool {
	c.c.valRecv = JudgeAction{}
	c.c.errRecv = c.c.Recv(&c.c.valRecv)
	return c.c.errRecv == nil
}
func (c implJudgePlayCallRecv) Value() JudgeAction {
	return c.c.valRecv
}
func (c implJudgePlayCallRecv) Err() error {
	if c.c.errRecv == __io.EOF {
		return nil
	}
	return c.c.errRecv
}
func (c *implJudgePlayCall) SendStream() interface {
	Send(item PlayerAction) error
	Close() error
} {
	return implJudgePlayCallSend{c}
}

type implJudgePlayCallSend struct {
	c *implJudgePlayCall
}

func (c implJudgePlayCallSend) Send(item PlayerAction) error {
	return c.c.Send(item)
}
func (c implJudgePlayCallSend) Close() error {
	return c.c.CloseSend()
}
func (c *implJudgePlayCall) Finish() (o0 PlayResult, err error) {
	if ierr := c.Call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

// JudgeServerMethods is the interface a server writer
// implements for Judge.
type JudgeServerMethods interface {
	// CreateGame creates a new game with the given game options and returns a game
	// identifier that can be used by the players to join the game.
	CreateGame(ctx __ipc.ServerContext, Opts GameOptions) (GameID, error)
	// Play lets a player join an existing game and play.
	Play(ctx JudgePlayContext, ID GameID) (PlayResult, error)
}

// JudgeServerStubMethods is the server interface containing
// Judge methods, as expected by ipc.Server.
// The only difference between this interface and JudgeServerMethods
// is the streaming methods.
type JudgeServerStubMethods interface {
	// CreateGame creates a new game with the given game options and returns a game
	// identifier that can be used by the players to join the game.
	CreateGame(ctx __ipc.ServerContext, Opts GameOptions) (GameID, error)
	// Play lets a player join an existing game and play.
	Play(ctx *JudgePlayContextStub, ID GameID) (PlayResult, error)
}

// JudgeServerStub adds universal methods to JudgeServerStubMethods.
type JudgeServerStub interface {
	JudgeServerStubMethods
	// Describe the Judge interfaces.
	Describe__() []__ipc.InterfaceDesc
	// Signature will be replaced with Describe__.
	Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error)
}

// JudgeServer returns a server stub for Judge.
// It converts an implementation of JudgeServerMethods into
// an object that may be used by ipc.Server.
func JudgeServer(impl JudgeServerMethods) JudgeServerStub {
	stub := implJudgeServerStub{
		impl: impl,
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := __ipc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := __ipc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implJudgeServerStub struct {
	impl JudgeServerMethods
	gs   *__ipc.GlobState
}

func (s implJudgeServerStub) CreateGame(ctx __ipc.ServerContext, i0 GameOptions) (GameID, error) {
	return s.impl.CreateGame(ctx, i0)
}

func (s implJudgeServerStub) Play(ctx *JudgePlayContextStub, i0 GameID) (PlayResult, error) {
	return s.impl.Play(ctx, i0)
}

func (s implJudgeServerStub) VGlob() *__ipc.GlobState {
	return s.gs
}

func (s implJudgeServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{JudgeDesc}
}

// JudgeDesc describes the Judge interface.
var JudgeDesc __ipc.InterfaceDesc = descJudge

// descJudge hides the desc to keep godoc clean.
var descJudge = __ipc.InterfaceDesc{
	Name:    "Judge",
	PkgPath: "veyron.io/apps/rps",
	Methods: []__ipc.MethodDesc{
		{
			Name: "CreateGame",
			Doc:  "// CreateGame creates a new game with the given game options and returns a game\n// identifier that can be used by the players to join the game.",
			InArgs: []__ipc.ArgDesc{
				{"Opts", ``}, // GameOptions
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // GameID
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Admin")},
		},
		{
			Name: "Play",
			Doc:  "// Play lets a player join an existing game and play.",
			InArgs: []__ipc.ArgDesc{
				{"ID", ``}, // GameID
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // PlayResult
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Admin")},
		},
	},
}

func (s implJudgeServerStub) Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error) {
	// TODO(toddw): Replace with new Describe__ implementation.
	result := __ipc.ServiceSignature{Methods: make(map[string]__ipc.MethodSignature)}
	result.Methods["CreateGame"] = __ipc.MethodSignature{
		InArgs: []__ipc.MethodArgument{
			{Name: "Opts", Type: 66},
		},
		OutArgs: []__ipc.MethodArgument{
			{Name: "", Type: 67},
			{Name: "", Type: 68},
		},
	}
	result.Methods["Play"] = __ipc.MethodSignature{
		InArgs: []__ipc.MethodArgument{
			{Name: "ID", Type: 67},
		},
		OutArgs: []__ipc.MethodArgument{
			{Name: "", Type: 69},
			{Name: "", Type: 68},
		},
		InStream:  70,
		OutStream: 76,
	}

	result.TypeDefs = []__vdlutil.Any{
		__wiretype.NamedPrimitiveType{Type: 0x32, Name: "veyron.io/apps/rps.GameTypeTag", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x24, Name: "NumRounds"},
				__wiretype.FieldType{Type: 0x41, Name: "GameType"},
			},
			"veyron.io/apps/rps.GameOptions", []string(nil)},
		__wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x3, Name: "ID"},
			},
			"veyron.io/apps/rps.GameID", []string(nil)},
		__wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x2, Name: "YouWon"},
			},
			"veyron.io/apps/rps.PlayResult", []string(nil)},
		__wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x3, Name: "Move"},
				__wiretype.FieldType{Type: 0x2, Name: "Quit"},
			},
			"veyron.io/apps/rps.PlayerAction", []string(nil)},
		__wiretype.ArrayType{Elem: 0x3, Len: 0x2, Name: "", Tags: []string(nil)}, __wiretype.NamedPrimitiveType{Type: 0x32, Name: "veyron.io/apps/rps.WinnerTag", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x47, Name: "Moves"},
				__wiretype.FieldType{Type: 0x3, Name: "Comment"},
				__wiretype.FieldType{Type: 0x48, Name: "Winner"},
				__wiretype.FieldType{Type: 0x25, Name: "StartTimeNS"},
				__wiretype.FieldType{Type: 0x25, Name: "EndTimeNS"},
			},
			"veyron.io/apps/rps.Round", []string(nil)},
		__wiretype.SliceType{Elem: 0x49, Name: "", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x42, Name: "Opts"},
				__wiretype.FieldType{Type: 0x3, Name: "Judge"},
				__wiretype.FieldType{Type: 0x3d, Name: "Players"},
				__wiretype.FieldType{Type: 0x4a, Name: "Rounds"},
				__wiretype.FieldType{Type: 0x25, Name: "StartTimeNS"},
				__wiretype.FieldType{Type: 0x25, Name: "EndTimeNS"},
				__wiretype.FieldType{Type: 0x48, Name: "Winner"},
			},
			"veyron.io/apps/rps.ScoreCard", []string(nil)},
		__wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x24, Name: "PlayerNum"},
				__wiretype.FieldType{Type: 0x3, Name: "OpponentName"},
				__wiretype.FieldType{Type: 0x3d, Name: "MoveOptions"},
				__wiretype.FieldType{Type: 0x49, Name: "RoundResult"},
				__wiretype.FieldType{Type: 0x4b, Name: "Score"},
			},
			"veyron.io/apps/rps.JudgeAction", []string(nil)},
	}

	return result, nil
}

// JudgePlayServerStream is the server stream for Judge.Play.
type JudgePlayServerStream interface {
	// RecvStream returns the receiver side of the Judge.Play server stream.
	RecvStream() interface {
		// Advance stages an item so that it may be retrieved via Value.  Returns
		// true iff there is an item to retrieve.  Advance must be called before
		// Value is called.  May block if an item is not available.
		Advance() bool
		// Value returns the item that was staged by Advance.  May panic if Advance
		// returned false or was not called.  Never blocks.
		Value() PlayerAction
		// Err returns any error encountered by Advance.  Never blocks.
		Err() error
	}
	// SendStream returns the send side of the Judge.Play server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item JudgeAction) error
	}
}

// JudgePlayContext represents the context passed to Judge.Play.
type JudgePlayContext interface {
	__ipc.ServerContext
	JudgePlayServerStream
}

// JudgePlayContextStub is a wrapper that converts ipc.ServerCall into
// a typesafe stub that implements JudgePlayContext.
type JudgePlayContextStub struct {
	__ipc.ServerCall
	valRecv PlayerAction
	errRecv error
}

// Init initializes JudgePlayContextStub from ipc.ServerCall.
func (s *JudgePlayContextStub) Init(call __ipc.ServerCall) {
	s.ServerCall = call
}

// RecvStream returns the receiver side of the Judge.Play server stream.
func (s *JudgePlayContextStub) RecvStream() interface {
	Advance() bool
	Value() PlayerAction
	Err() error
} {
	return implJudgePlayContextRecv{s}
}

type implJudgePlayContextRecv struct {
	s *JudgePlayContextStub
}

func (s implJudgePlayContextRecv) Advance() bool {
	s.s.valRecv = PlayerAction{}
	s.s.errRecv = s.s.Recv(&s.s.valRecv)
	return s.s.errRecv == nil
}
func (s implJudgePlayContextRecv) Value() PlayerAction {
	return s.s.valRecv
}
func (s implJudgePlayContextRecv) Err() error {
	if s.s.errRecv == __io.EOF {
		return nil
	}
	return s.s.errRecv
}

// SendStream returns the send side of the Judge.Play server stream.
func (s *JudgePlayContextStub) SendStream() interface {
	Send(item JudgeAction) error
} {
	return implJudgePlayContextSend{s}
}

type implJudgePlayContextSend struct {
	s *JudgePlayContextStub
}

func (s implJudgePlayContextSend) Send(item JudgeAction) error {
	return s.s.Send(item)
}

// PlayerClientMethods is the client interface
// containing Player methods.
//
// Player can receive challenges from other players.
type PlayerClientMethods interface {
	// Challenge is used by other players to challenge this player to a game. If
	// the challenge is accepted, the method returns nil.
	Challenge(ctx __context.T, Address string, ID GameID, Opts GameOptions, opts ...__ipc.CallOpt) error
}

// PlayerClientStub adds universal methods to PlayerClientMethods.
type PlayerClientStub interface {
	PlayerClientMethods
	__ipc.UniversalServiceMethods
}

// PlayerClient returns a client stub for Player.
func PlayerClient(name string, opts ...__ipc.BindOpt) PlayerClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implPlayerClientStub{name, client}
}

type implPlayerClientStub struct {
	name   string
	client __ipc.Client
}

func (c implPlayerClientStub) c(ctx __context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.RuntimeFromContext(ctx).Client()
}

func (c implPlayerClientStub) Challenge(ctx __context.T, i0 string, i1 GameID, i2 GameOptions, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Challenge", []interface{}{i0, i1, i2}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c implPlayerClientStub) Signature(ctx __context.T, opts ...__ipc.CallOpt) (o0 __ipc.ServiceSignature, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

// PlayerServerMethods is the interface a server writer
// implements for Player.
//
// Player can receive challenges from other players.
type PlayerServerMethods interface {
	// Challenge is used by other players to challenge this player to a game. If
	// the challenge is accepted, the method returns nil.
	Challenge(ctx __ipc.ServerContext, Address string, ID GameID, Opts GameOptions) error
}

// PlayerServerStubMethods is the server interface containing
// Player methods, as expected by ipc.Server.
// There is no difference between this interface and PlayerServerMethods
// since there are no streaming methods.
type PlayerServerStubMethods PlayerServerMethods

// PlayerServerStub adds universal methods to PlayerServerStubMethods.
type PlayerServerStub interface {
	PlayerServerStubMethods
	// Describe the Player interfaces.
	Describe__() []__ipc.InterfaceDesc
	// Signature will be replaced with Describe__.
	Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error)
}

// PlayerServer returns a server stub for Player.
// It converts an implementation of PlayerServerMethods into
// an object that may be used by ipc.Server.
func PlayerServer(impl PlayerServerMethods) PlayerServerStub {
	stub := implPlayerServerStub{
		impl: impl,
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := __ipc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := __ipc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implPlayerServerStub struct {
	impl PlayerServerMethods
	gs   *__ipc.GlobState
}

func (s implPlayerServerStub) Challenge(ctx __ipc.ServerContext, i0 string, i1 GameID, i2 GameOptions) error {
	return s.impl.Challenge(ctx, i0, i1, i2)
}

func (s implPlayerServerStub) VGlob() *__ipc.GlobState {
	return s.gs
}

func (s implPlayerServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{PlayerDesc}
}

// PlayerDesc describes the Player interface.
var PlayerDesc __ipc.InterfaceDesc = descPlayer

// descPlayer hides the desc to keep godoc clean.
var descPlayer = __ipc.InterfaceDesc{
	Name:    "Player",
	PkgPath: "veyron.io/apps/rps",
	Doc:     "// Player can receive challenges from other players.",
	Methods: []__ipc.MethodDesc{
		{
			Name: "Challenge",
			Doc:  "// Challenge is used by other players to challenge this player to a game. If\n// the challenge is accepted, the method returns nil.",
			InArgs: []__ipc.ArgDesc{
				{"Address", ``}, // string
				{"ID", ``},      // GameID
				{"Opts", ``},    // GameOptions
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Admin")},
		},
	},
}

func (s implPlayerServerStub) Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error) {
	// TODO(toddw): Replace with new Describe__ implementation.
	result := __ipc.ServiceSignature{Methods: make(map[string]__ipc.MethodSignature)}
	result.Methods["Challenge"] = __ipc.MethodSignature{
		InArgs: []__ipc.MethodArgument{
			{Name: "Address", Type: 3},
			{Name: "ID", Type: 65},
			{Name: "Opts", Type: 67},
		},
		OutArgs: []__ipc.MethodArgument{
			{Name: "", Type: 68},
		},
	}

	result.TypeDefs = []__vdlutil.Any{
		__wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x3, Name: "ID"},
			},
			"veyron.io/apps/rps.GameID", []string(nil)},
		__wiretype.NamedPrimitiveType{Type: 0x32, Name: "veyron.io/apps/rps.GameTypeTag", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x24, Name: "NumRounds"},
				__wiretype.FieldType{Type: 0x42, Name: "GameType"},
			},
			"veyron.io/apps/rps.GameOptions", []string(nil)},
		__wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

// ScoreKeeperClientMethods is the client interface
// containing ScoreKeeper methods.
//
// ScoreKeeper receives the outcome of games from Judges.
type ScoreKeeperClientMethods interface {
	Record(ctx __context.T, Score ScoreCard, opts ...__ipc.CallOpt) error
}

// ScoreKeeperClientStub adds universal methods to ScoreKeeperClientMethods.
type ScoreKeeperClientStub interface {
	ScoreKeeperClientMethods
	__ipc.UniversalServiceMethods
}

// ScoreKeeperClient returns a client stub for ScoreKeeper.
func ScoreKeeperClient(name string, opts ...__ipc.BindOpt) ScoreKeeperClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implScoreKeeperClientStub{name, client}
}

type implScoreKeeperClientStub struct {
	name   string
	client __ipc.Client
}

func (c implScoreKeeperClientStub) c(ctx __context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.RuntimeFromContext(ctx).Client()
}

func (c implScoreKeeperClientStub) Record(ctx __context.T, i0 ScoreCard, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Record", []interface{}{i0}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c implScoreKeeperClientStub) Signature(ctx __context.T, opts ...__ipc.CallOpt) (o0 __ipc.ServiceSignature, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

// ScoreKeeperServerMethods is the interface a server writer
// implements for ScoreKeeper.
//
// ScoreKeeper receives the outcome of games from Judges.
type ScoreKeeperServerMethods interface {
	Record(ctx __ipc.ServerContext, Score ScoreCard) error
}

// ScoreKeeperServerStubMethods is the server interface containing
// ScoreKeeper methods, as expected by ipc.Server.
// There is no difference between this interface and ScoreKeeperServerMethods
// since there are no streaming methods.
type ScoreKeeperServerStubMethods ScoreKeeperServerMethods

// ScoreKeeperServerStub adds universal methods to ScoreKeeperServerStubMethods.
type ScoreKeeperServerStub interface {
	ScoreKeeperServerStubMethods
	// Describe the ScoreKeeper interfaces.
	Describe__() []__ipc.InterfaceDesc
	// Signature will be replaced with Describe__.
	Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error)
}

// ScoreKeeperServer returns a server stub for ScoreKeeper.
// It converts an implementation of ScoreKeeperServerMethods into
// an object that may be used by ipc.Server.
func ScoreKeeperServer(impl ScoreKeeperServerMethods) ScoreKeeperServerStub {
	stub := implScoreKeeperServerStub{
		impl: impl,
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := __ipc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := __ipc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implScoreKeeperServerStub struct {
	impl ScoreKeeperServerMethods
	gs   *__ipc.GlobState
}

func (s implScoreKeeperServerStub) Record(ctx __ipc.ServerContext, i0 ScoreCard) error {
	return s.impl.Record(ctx, i0)
}

func (s implScoreKeeperServerStub) VGlob() *__ipc.GlobState {
	return s.gs
}

func (s implScoreKeeperServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{ScoreKeeperDesc}
}

// ScoreKeeperDesc describes the ScoreKeeper interface.
var ScoreKeeperDesc __ipc.InterfaceDesc = descScoreKeeper

// descScoreKeeper hides the desc to keep godoc clean.
var descScoreKeeper = __ipc.InterfaceDesc{
	Name:    "ScoreKeeper",
	PkgPath: "veyron.io/apps/rps",
	Doc:     "// ScoreKeeper receives the outcome of games from Judges.",
	Methods: []__ipc.MethodDesc{
		{
			Name: "Record",
			InArgs: []__ipc.ArgDesc{
				{"Score", ``}, // ScoreCard
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Admin")},
		},
	},
}

func (s implScoreKeeperServerStub) Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error) {
	// TODO(toddw): Replace with new Describe__ implementation.
	result := __ipc.ServiceSignature{Methods: make(map[string]__ipc.MethodSignature)}
	result.Methods["Record"] = __ipc.MethodSignature{
		InArgs: []__ipc.MethodArgument{
			{Name: "Score", Type: 71},
		},
		OutArgs: []__ipc.MethodArgument{
			{Name: "", Type: 72},
		},
	}

	result.TypeDefs = []__vdlutil.Any{
		__wiretype.NamedPrimitiveType{Type: 0x32, Name: "veyron.io/apps/rps.GameTypeTag", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x24, Name: "NumRounds"},
				__wiretype.FieldType{Type: 0x41, Name: "GameType"},
			},
			"veyron.io/apps/rps.GameOptions", []string(nil)},
		__wiretype.ArrayType{Elem: 0x3, Len: 0x2, Name: "", Tags: []string(nil)}, __wiretype.NamedPrimitiveType{Type: 0x32, Name: "veyron.io/apps/rps.WinnerTag", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x43, Name: "Moves"},
				__wiretype.FieldType{Type: 0x3, Name: "Comment"},
				__wiretype.FieldType{Type: 0x44, Name: "Winner"},
				__wiretype.FieldType{Type: 0x25, Name: "StartTimeNS"},
				__wiretype.FieldType{Type: 0x25, Name: "EndTimeNS"},
			},
			"veyron.io/apps/rps.Round", []string(nil)},
		__wiretype.SliceType{Elem: 0x45, Name: "", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x42, Name: "Opts"},
				__wiretype.FieldType{Type: 0x3, Name: "Judge"},
				__wiretype.FieldType{Type: 0x3d, Name: "Players"},
				__wiretype.FieldType{Type: 0x46, Name: "Rounds"},
				__wiretype.FieldType{Type: 0x25, Name: "StartTimeNS"},
				__wiretype.FieldType{Type: 0x25, Name: "EndTimeNS"},
				__wiretype.FieldType{Type: 0x44, Name: "Winner"},
			},
			"veyron.io/apps/rps.ScoreCard", []string(nil)},
		__wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

// RockPaperScissorsClientMethods is the client interface
// containing RockPaperScissors methods.
type RockPaperScissorsClientMethods interface {
	JudgeClientMethods
	// Player can receive challenges from other players.
	PlayerClientMethods
	// ScoreKeeper receives the outcome of games from Judges.
	ScoreKeeperClientMethods
}

// RockPaperScissorsClientStub adds universal methods to RockPaperScissorsClientMethods.
type RockPaperScissorsClientStub interface {
	RockPaperScissorsClientMethods
	__ipc.UniversalServiceMethods
}

// RockPaperScissorsClient returns a client stub for RockPaperScissors.
func RockPaperScissorsClient(name string, opts ...__ipc.BindOpt) RockPaperScissorsClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implRockPaperScissorsClientStub{name, client, JudgeClient(name, client), PlayerClient(name, client), ScoreKeeperClient(name, client)}
}

type implRockPaperScissorsClientStub struct {
	name   string
	client __ipc.Client

	JudgeClientStub
	PlayerClientStub
	ScoreKeeperClientStub
}

func (c implRockPaperScissorsClientStub) c(ctx __context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.RuntimeFromContext(ctx).Client()
}

func (c implRockPaperScissorsClientStub) Signature(ctx __context.T, opts ...__ipc.CallOpt) (o0 __ipc.ServiceSignature, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

// RockPaperScissorsServerMethods is the interface a server writer
// implements for RockPaperScissors.
type RockPaperScissorsServerMethods interface {
	JudgeServerMethods
	// Player can receive challenges from other players.
	PlayerServerMethods
	// ScoreKeeper receives the outcome of games from Judges.
	ScoreKeeperServerMethods
}

// RockPaperScissorsServerStubMethods is the server interface containing
// RockPaperScissors methods, as expected by ipc.Server.
// The only difference between this interface and RockPaperScissorsServerMethods
// is the streaming methods.
type RockPaperScissorsServerStubMethods interface {
	JudgeServerStubMethods
	// Player can receive challenges from other players.
	PlayerServerStubMethods
	// ScoreKeeper receives the outcome of games from Judges.
	ScoreKeeperServerStubMethods
}

// RockPaperScissorsServerStub adds universal methods to RockPaperScissorsServerStubMethods.
type RockPaperScissorsServerStub interface {
	RockPaperScissorsServerStubMethods
	// Describe the RockPaperScissors interfaces.
	Describe__() []__ipc.InterfaceDesc
	// Signature will be replaced with Describe__.
	Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error)
}

// RockPaperScissorsServer returns a server stub for RockPaperScissors.
// It converts an implementation of RockPaperScissorsServerMethods into
// an object that may be used by ipc.Server.
func RockPaperScissorsServer(impl RockPaperScissorsServerMethods) RockPaperScissorsServerStub {
	stub := implRockPaperScissorsServerStub{
		impl:                  impl,
		JudgeServerStub:       JudgeServer(impl),
		PlayerServerStub:      PlayerServer(impl),
		ScoreKeeperServerStub: ScoreKeeperServer(impl),
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := __ipc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := __ipc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implRockPaperScissorsServerStub struct {
	impl RockPaperScissorsServerMethods
	JudgeServerStub
	PlayerServerStub
	ScoreKeeperServerStub
	gs *__ipc.GlobState
}

func (s implRockPaperScissorsServerStub) VGlob() *__ipc.GlobState {
	return s.gs
}

func (s implRockPaperScissorsServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{RockPaperScissorsDesc, JudgeDesc, PlayerDesc, ScoreKeeperDesc}
}

// RockPaperScissorsDesc describes the RockPaperScissors interface.
var RockPaperScissorsDesc __ipc.InterfaceDesc = descRockPaperScissors

// descRockPaperScissors hides the desc to keep godoc clean.
var descRockPaperScissors = __ipc.InterfaceDesc{
	Name:    "RockPaperScissors",
	PkgPath: "veyron.io/apps/rps",
	Embeds: []__ipc.EmbedDesc{
		{"Judge", "veyron.io/apps/rps", ``},
		{"Player", "veyron.io/apps/rps", "// Player can receive challenges from other players."},
		{"ScoreKeeper", "veyron.io/apps/rps", "// ScoreKeeper receives the outcome of games from Judges."},
	},
}

func (s implRockPaperScissorsServerStub) Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error) {
	// TODO(toddw): Replace with new Describe__ implementation.
	result := __ipc.ServiceSignature{Methods: make(map[string]__ipc.MethodSignature)}

	result.TypeDefs = []__vdlutil.Any{}
	var ss __ipc.ServiceSignature
	var firstAdded int
	ss, _ = s.JudgeServerStub.Signature(ctx)
	firstAdded = len(result.TypeDefs)
	for k, v := range ss.Methods {
		for i, _ := range v.InArgs {
			if v.InArgs[i].Type >= __wiretype.TypeIDFirst {
				v.InArgs[i].Type += __wiretype.TypeID(firstAdded)
			}
		}
		for i, _ := range v.OutArgs {
			if v.OutArgs[i].Type >= __wiretype.TypeIDFirst {
				v.OutArgs[i].Type += __wiretype.TypeID(firstAdded)
			}
		}
		if v.InStream >= __wiretype.TypeIDFirst {
			v.InStream += __wiretype.TypeID(firstAdded)
		}
		if v.OutStream >= __wiretype.TypeIDFirst {
			v.OutStream += __wiretype.TypeID(firstAdded)
		}
		result.Methods[k] = v
	}
	//TODO(bprosnitz) combine type definitions from embeded interfaces in a way that doesn't cause duplication.
	for _, d := range ss.TypeDefs {
		switch wt := d.(type) {
		case __wiretype.SliceType:
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.ArrayType:
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.MapType:
			if wt.Key >= __wiretype.TypeIDFirst {
				wt.Key += __wiretype.TypeID(firstAdded)
			}
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.StructType:
			for i, fld := range wt.Fields {
				if fld.Type >= __wiretype.TypeIDFirst {
					wt.Fields[i].Type += __wiretype.TypeID(firstAdded)
				}
			}
			d = wt
			// NOTE: other types are missing, but we are upgrading anyways.
		}
		result.TypeDefs = append(result.TypeDefs, d)
	}
	ss, _ = s.PlayerServerStub.Signature(ctx)
	firstAdded = len(result.TypeDefs)
	for k, v := range ss.Methods {
		for i, _ := range v.InArgs {
			if v.InArgs[i].Type >= __wiretype.TypeIDFirst {
				v.InArgs[i].Type += __wiretype.TypeID(firstAdded)
			}
		}
		for i, _ := range v.OutArgs {
			if v.OutArgs[i].Type >= __wiretype.TypeIDFirst {
				v.OutArgs[i].Type += __wiretype.TypeID(firstAdded)
			}
		}
		if v.InStream >= __wiretype.TypeIDFirst {
			v.InStream += __wiretype.TypeID(firstAdded)
		}
		if v.OutStream >= __wiretype.TypeIDFirst {
			v.OutStream += __wiretype.TypeID(firstAdded)
		}
		result.Methods[k] = v
	}
	//TODO(bprosnitz) combine type definitions from embeded interfaces in a way that doesn't cause duplication.
	for _, d := range ss.TypeDefs {
		switch wt := d.(type) {
		case __wiretype.SliceType:
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.ArrayType:
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.MapType:
			if wt.Key >= __wiretype.TypeIDFirst {
				wt.Key += __wiretype.TypeID(firstAdded)
			}
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.StructType:
			for i, fld := range wt.Fields {
				if fld.Type >= __wiretype.TypeIDFirst {
					wt.Fields[i].Type += __wiretype.TypeID(firstAdded)
				}
			}
			d = wt
			// NOTE: other types are missing, but we are upgrading anyways.
		}
		result.TypeDefs = append(result.TypeDefs, d)
	}
	ss, _ = s.ScoreKeeperServerStub.Signature(ctx)
	firstAdded = len(result.TypeDefs)
	for k, v := range ss.Methods {
		for i, _ := range v.InArgs {
			if v.InArgs[i].Type >= __wiretype.TypeIDFirst {
				v.InArgs[i].Type += __wiretype.TypeID(firstAdded)
			}
		}
		for i, _ := range v.OutArgs {
			if v.OutArgs[i].Type >= __wiretype.TypeIDFirst {
				v.OutArgs[i].Type += __wiretype.TypeID(firstAdded)
			}
		}
		if v.InStream >= __wiretype.TypeIDFirst {
			v.InStream += __wiretype.TypeID(firstAdded)
		}
		if v.OutStream >= __wiretype.TypeIDFirst {
			v.OutStream += __wiretype.TypeID(firstAdded)
		}
		result.Methods[k] = v
	}
	//TODO(bprosnitz) combine type definitions from embeded interfaces in a way that doesn't cause duplication.
	for _, d := range ss.TypeDefs {
		switch wt := d.(type) {
		case __wiretype.SliceType:
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.ArrayType:
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.MapType:
			if wt.Key >= __wiretype.TypeIDFirst {
				wt.Key += __wiretype.TypeID(firstAdded)
			}
			if wt.Elem >= __wiretype.TypeIDFirst {
				wt.Elem += __wiretype.TypeID(firstAdded)
			}
			d = wt
		case __wiretype.StructType:
			for i, fld := range wt.Fields {
				if fld.Type >= __wiretype.TypeIDFirst {
					wt.Fields[i].Type += __wiretype.TypeID(firstAdded)
				}
			}
			d = wt
			// NOTE: other types are missing, but we are upgrading anyways.
		}
		result.TypeDefs = append(result.TypeDefs, d)
	}

	return result, nil
}
