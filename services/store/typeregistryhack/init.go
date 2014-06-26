// Package typeregistryhack registers types that client send to the store, for
// VOM cannot decode objects of unknown type.
//
// TODO(tilaks): use val.Value to decode unknown types.
package typeregistryhack

import (
	"veyron/services/mgmt/profile"
	"veyron2/services/mgmt/application"

	// Register boxes types
	"veyron/examples/boxes"
	// Register mdb types.
	_ "veyron/examples/storage/mdb/schema"
	// Register todos types.
	_ "veyron/examples/todos/schema"
	// Register bank types.
	_ "veyron/examples/bank/schema"
	// Register stfortune types.
	_ "veyron/examples/stfortune/schema"

	"veyron2/vom"
)

func init() {
	// Register profile types.
	vom.Register(&struct{}{}) // directories have type struct{}.
	vom.Register(&profile.Specification{})
	vom.Register(&application.Envelope{})
	vom.Register(&boxes.Box{})
}
