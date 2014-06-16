// This schema registers structs used in the bank example to the VOM for the Veyron store.
package schema

import (
	"veyron2/vom"
)

func init() {
	vom.Register(&Dir{})
	vom.Register(&Bank{})
}
