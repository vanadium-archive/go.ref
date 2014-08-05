// Package typeregistryhack registers types that client send to the store, for
// VOM cannot decode objects of unknown type.
//
// TODO(tilaks): use val.Value to decode unknown types.
package typeregistryhack

import (
	// Register boxes types.
	"veyron/examples/boxes"
	// Register mdb types.
	_ "veyron/examples/mdb/schema"
	// Register todos types.
	_ "veyron/examples/todos/schema"
	// Register bank types.
	_ "veyron/examples/bank/schema"
	// Register stfortune types.
	_ "veyron/examples/stfortune/schema"
	// Register profile types.
	"veyron/services/mgmt/profile"
	// Register application types.
	"veyron2/services/mgmt/application"
	// Register build types.
	_ "veyron2/services/mgmt/build"

	"veyron2/vom"
)

func init() {
	// Register profile types.
	vom.Register(&struct{}{}) // directories have type struct{}.
	vom.Register(&profile.Specification{})
	vom.Register(&application.Envelope{})
	vom.Register(&boxes.Box{})
}
