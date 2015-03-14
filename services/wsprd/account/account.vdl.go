// This file was auto-generated by the vanadium vdl tool.
// Source: account.vdl

package account

import (
	// VDL system imports
	"v.io/v23/vdl"
)

// Caveat describes a restriction on the validity of a blessing/discharge.
type Caveat struct {
	Type string
	Args string
}

func (Caveat) __VDLReflect(struct {
	Name string "v.io/x/ref/services/wsprd/account.Caveat"
}) {
}

func init() {
	vdl.Register((*Caveat)(nil))
}
