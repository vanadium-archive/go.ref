// Package schema defines a schema for a todos application.
package schema

import (
	"veyron2/vom"
)

func init() {
	vom.Register(&Dir{})
	vom.Register(&List{})
	vom.Register(&Item{})
}
