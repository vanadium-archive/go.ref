package discharger

import (
	"fmt"
	"time"

	services "v.io/core/veyron/services/security"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
)

// dischargerd issues discharges for all caveats present in the current
// namespace with no additional caveats iff the caveat is valid.
type dischargerd struct{}

func (dischargerd) Discharge(ctx ipc.ServerContext, caveat security.Caveat, _ security.DischargeImpetus) (vdl.AnyRep, error) {
	tp := caveat.ThirdPartyDetails()
	if tp == nil {
		return nil, fmt.Errorf("Caveat %v does not represent a third party caveat", caveat)
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
