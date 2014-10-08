package lib

import (
	// Any non-release package imports are not allowed to make
	// sure the repository can be downloaded using "go get".
	// rps "veyron.io/examples/rockpaperscissors"
	// "veyron.io/store/veyron2/services/store"
	// roadmap_watchtypes "veyron.io/store/veyron2/services/watch/types"
	// "veyron.io/store/veyron2/storage"

	mttypes "veyron.io/veyron/veyron2/services/mounttable/types"
	watchtypes "veyron.io/veyron/veyron2/services/watch/types"
	"veyron.io/veyron/veyron2/vom"
)

func init() {
	vom.Register(mttypes.MountEntry{})
	vom.Register(watchtypes.GlobRequest{})
	vom.Register(watchtypes.Change{})
	// vom.Register(storage.Entry{})
	// vom.Register(storage.Stat{})
	// vom.Register(store.NestedResult(0))
	// vom.Register(store.QueryResult{})
	// vom.Register(roadmap_watchtypes.QueryRequest{})
	// vom.Register(rps.GameOptions{})
	// vom.Register(rps.GameID{})
	// vom.Register(rps.PlayResult{})
	// vom.Register(rps.PlayerAction{})
	// vom.Register(rps.JudgeAction{})
}
