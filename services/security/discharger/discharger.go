package discharger

import (
	"fmt"
	"time"

	services "v.io/core/veyron/services/security"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vom"
)

// dischargerd issues discharges for all caveats present in the current
// namespace with no additional caveats iff the caveat is valid.
type dischargerd struct{}

func (dischargerd) Discharge(ctx ipc.ServerContext, caveatAny vdl.AnyRep, _ security.DischargeImpetus) (vdl.AnyRep, error) {
	// TODO(ashankar): When security.Caveat.ValidatorVOM goes away
	// (before the release), then this whole "if..else if" block below
	// should vanish, we'll start with the:
	// tp := caveat.ThirdPartyDetails()
	// line (and "caveatAny vdl.AnyRep" will become "caveat security.Caveat")
	var caveat security.Caveat
	if c, ok := caveatAny.(security.Caveat); ok {
		caveat = c
	} else if tp, ok := caveatAny.(security.ThirdPartyCaveat); ok {
		// This whole block is a temporary hack that works
		// because there is only a single valid implementation
		// of security.ThirdPartyCaveat.
		// It will go away before the release. See TODO above.
		copy(caveat.Id[:], security.PublicKeyThirdPartyCaveatX.Id[:])
		var err error
		if caveat.ParamVom, err = vom.Encode(tp); err != nil {
			return nil, fmt.Errorf("hack error: %v", err)
		}
	}
	tp := caveat.ThirdPartyDetails()
	if tp == nil {
		return nil, fmt.Errorf("type %T(%v) does not represent a third party caveat")
	}
	if err := tp.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", tp, err)
	}
	expiry, err := security.ExpiryCaveat(time.Now().Add(15 * time.Minute))
	if err != nil {
		return nil, fmt.Errorf("unable to create expiration caveat on the discharge: %v", err)
	}
	return ctx.LocalPrincipal().MintDischarge(caveat, expiry)
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
