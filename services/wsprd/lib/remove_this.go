package lib

import (
	rps "veyron/examples/rockpaperscissors"
	"veyron2/services/mounttable"
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/vom"
)

func init() {
	vom.Register(mounttable.MountEntry{})
	vom.Register(store.Conflict{})
	vom.Register(store.Entry{})
	vom.Register(store.NestedResult(0))
	vom.Register(store.QueryResult{})
	vom.Register(store.Stat{})
	vom.Register(store.TransactionID(0))
	vom.Register(watch.GlobRequest{})
	vom.Register(watch.QueryRequest{})
	vom.Register(watch.ChangeBatch{})
	vom.Register(watch.Change{})
	vom.Register(rps.GameOptions{})
	vom.Register(rps.GameID{})
	vom.Register(rps.PlayResult{})
	vom.Register(rps.PlayerAction{})
	vom.Register(rps.JudgeAction{})

}
