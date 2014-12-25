package lib

import (
	// Any non-release package imports are not allowed to make
	// sure the repository can be downloaded using "go get".
	// rps "v.io/examples/rockpaperscissors"
	// "v.io/store/veyron2/services/store"
	// roadmap_watchtypes "v.io/store/veyron2/services/watch/types"
	// "v.io/store/veyron2/storage"

	"v.io/veyron/veyron2/naming"
	watchtypes "v.io/veyron/veyron2/services/watch/types"
	"v.io/veyron/veyron2/vom"
)

func init() {
	vom.Register(naming.VDLMountEntry{})
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
