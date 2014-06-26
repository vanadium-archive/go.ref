package schema

import (
	"veyron2/vom"
)

func init() {
	vom.Register(&FortuneData{})
	vom.Register(&User{})
}
