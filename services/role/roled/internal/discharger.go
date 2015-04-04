// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/ref/services/discharger"

	"v.io/x/lib/vlog"
)

func init() {
	security.RegisterCaveatValidator(LoggingCaveat, func(ctx *context.T, params []string) error {
		vlog.Infof("Params: %#v", params)
		return nil
	})

}

type dischargerImpl struct{}

func (dischargerImpl) Discharge(call rpc.ServerCall, caveat security.Caveat, impetus security.DischargeImpetus) (security.Discharge, error) {
	details := caveat.ThirdPartyDetails()
	if details == nil {
		return security.Discharge{}, discharger.NewErrNotAThirdPartyCaveat(call.Context(), caveat)
	}
	if err := details.Dischargeable(call.Context()); err != nil {
		return security.Discharge{}, err
	}
	// TODO(rthellend,ashankar): Do proper logging when the API allows it.
	vlog.Infof("Discharge() impetus: %#v", impetus)

	expiry, err := security.ExpiryCaveat(time.Now().Add(5 * time.Minute))
	if err != nil {
		return security.Discharge{}, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	// Bind the discharge to precisely the purpose the requestor claims it will be used.
	method, err := security.MethodCaveat(impetus.Method)
	if err != nil {
		return security.Discharge{}, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	peer, err := security.NewCaveat(security.PeerBlessingsCaveat, impetus.Server)
	if err != nil {
		return security.Discharge{}, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	discharge, err := v23.GetPrincipal(call.Context()).MintDischarge(caveat, expiry, method, peer)
	if err != nil {
		return security.Discharge{}, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return discharge, nil
}
