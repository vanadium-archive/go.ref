package lib

import (
	"veyron2/services/store"
	"veyron2/services/watch"
	"veyron2/vom"
)

func init() {
	vom.Register(store.Conflict{})
	vom.Register(store.Entry{})
	vom.Register(store.NestedResult(0))
	vom.Register(store.QueryResult{})
	vom.Register(store.Stat{})
	vom.Register(store.TransactionID(0))
	vom.Register(watch.Request{})
	vom.Register(watch.ChangeBatch{})
	vom.Register(watch.Change{})
}
