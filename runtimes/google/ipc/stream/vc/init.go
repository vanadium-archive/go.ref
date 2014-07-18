package vc

import (
	isecurity "veyron/runtimes/google/security"

	"veyron2/security"
	"veyron2/vlog"
)

var anonymousID security.PrivateID

func init() {
	var err error
	if anonymousID, err = isecurity.NewPrivateID("anonymous"); err != nil {
		vlog.Fatalf("could create anonymousID for IPCs: %s", err)
	}
}
