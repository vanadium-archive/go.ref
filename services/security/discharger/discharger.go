package discharger

import (
	"fmt"
	"time"

	services "veyron.io/veyron/veyron/services/security"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
)

// dischargerd issues discharges for all caveats present in the current
// namespace with no additional caveats iff the caveat is valid.
type dischargerd struct{}

// TODO(andreser,ataly): make it easier for third party public key caveats to specify the caveats on their discharges

func (dischargerd) Discharge(ctx ipc.ServerContext, caveatAny vdlutil.Any, _ security.DischargeImpetus) (vdlutil.Any, error) {
	caveat, ok := caveatAny.(security.ThirdPartyCaveat)
	if !ok {
		return nil, fmt.Errorf("type %T does not implement security.ThirdPartyCaveat", caveatAny)
	}
	if err := caveat.Dischargeable(ctx); err != nil {
		return nil, fmt.Errorf("third-party caveat %v cannot be discharged for this context: %v", caveat, err)
	}
	expiry, err := security.ExpiryCaveat(time.Now().Add(time.Minute))
	if err != nil {
		return nil, fmt.Errorf("unable to create expiration caveat on the discharge: %v", err)
	}
	return ctx.LocalPrincipal().MintDischarge(caveat, expiry)
}

// NewDischarger returns a discharger service implementation that grants discharges using the MintDischarge
// on the principal receiving the RPC.
func NewDischarger() services.DischargerService {
	return dischargerd{}
}
