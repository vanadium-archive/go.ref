package vc

import (
	isecurity "veyron.io/veyron/veyron/runtimes/google/security"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

var anonymousID security.PrivateID

func init() {
	var err error
	if anonymousID, err = isecurity.NewPrivateID("anonymous", nil); err != nil {
		vlog.Fatalf("could not create anonymousID for IPCs: %s", err)
	}
}
