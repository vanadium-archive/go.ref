package lib

import (
	rps "veyron/examples/rockpaperscissors"
	mttypes "veyron2/services/mounttable/types"
	"veyron2/services/store"
	watchtypes "veyron2/services/watch/types"
	"veyron2/storage"
	"veyron2/vom"
)

func init() {
	vom.Register(mttypes.MountEntry{})
	vom.Register(storage.Entry{})
	vom.Register(storage.Stat{})
	vom.Register(store.NestedResult(0))
	vom.Register(store.QueryResult{})
	vom.Register(watchtypes.GlobRequest{})
	vom.Register(watchtypes.QueryRequest{})
	vom.Register(watchtypes.Change{})
	vom.Register(rps.GameOptions{})
	vom.Register(rps.GameID{})
	vom.Register(rps.PlayResult{})
	vom.Register(rps.PlayerAction{})
	vom.Register(rps.JudgeAction{})

}
