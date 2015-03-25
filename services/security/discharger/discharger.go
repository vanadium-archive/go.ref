// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discharger

import (
	"fmt"
	"time"

	"v.io/v23/rpc"
	"v.io/v23/security"
	services "v.io/x/ref/services/security"
)

// dischargerd issues discharges for all caveats present in the current
// namespace with no additional caveats iff the caveat is valid.
type dischargerd struct{}

func (dischargerd) Discharge(call rpc.ServerCall, caveat security.Caveat, _ security.DischargeImpetus) (security.Discharge, error) {
	ctx := call.Context()
	secCall := security.GetCall(ctx)
	tp := caveat.ThirdPartyDetails()
	if tp == nil {
		return security.Discharge{}, fmt.Errorf("Caveat %v does not represent a third party caveat", caveat)
	}
	if err := tp.Dischargeable(ctx); err != nil {
		return security.Discharge{}, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", tp, err)
	}
	expiry, err := security.ExpiryCaveat(time.Now().Add(15 * time.Minute))
	if err != nil {
		return security.Discharge{}, fmt.Errorf("unable to create expiration caveat on the discharge: %v", err)
	}
	return secCall.LocalPrincipal().MintDischarge(caveat, expiry)
}

// NewDischarger returns a discharger service implementation that grants
// discharges using the MintDischarge on the principal receiving the RPC.
//
// Discharges are valid for 15 minutes.
// TODO(ashankar,ataly): Parameterize this? Make it easier for clients to add
// caveats on the discharge?
func NewDischarger() services.DischargerServerMethods {
	return dischargerd{}
}
