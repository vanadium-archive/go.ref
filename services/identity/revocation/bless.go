package revocation

import (
	"fmt"
	"time"

	"veyron/security/audit"
	"veyron/security/caveat"

	"veyron2/security"
)

// Bless creates a blessing on behalf of the identity server.
func Bless(server security.PrivateID, blessee security.PublicID, email string, duration time.Duration, revocationCaveat security.ThirdPartyCaveat) (security.PublicID, error) {
	if revocationCaveat != nil {
		// TODO(suharshs): Extend the duration for blessings with provided revocaionCaveats
		return server.Bless(blessee, email, duration, []security.ServiceCaveat{caveat.UniversalCaveat(revocationCaveat)})
	}
	// return a blessing with a more limited duration, since there is no revocation caveat
	return server.Bless(blessee, email, duration, nil)
}

type BlessingAuditEntry struct {
	Blessee, Blessed security.PublicID
	Start, End       time.Time
	RevocationCaveat security.ThirdPartyCaveat
}

// ReadBlessAuditEntry is for use in the googleauth.handler to parse the arguments to the Bless call in util.Bless.
func ReadBlessAuditEntry(entry audit.Entry) (BlessingAuditEntry, error) {
	var blessEntry BlessingAuditEntry

	if len(entry.Arguments) < 4 || len(entry.Results) < 1 {
		return blessEntry, fmt.Errorf("entry is invalid format")
	}

	blessEntry.Blessee, _ = entry.Arguments[0].(security.PublicID)
	blessEntry.Start = entry.Timestamp
	if duration, ok := entry.Arguments[2].(int64); ok {
		blessEntry.End = blessEntry.Start.Add(time.Duration(duration))
	}
	blessEntry.Blessed, _ = entry.Results[0].(security.PublicID)
	caveats, _ := entry.Arguments[3].([]security.ServiceCaveat)
	if len(caveats) > 0 {
		blessEntry.RevocationCaveat, _ = caveats[0].Caveat.(security.ThirdPartyCaveat)
	}
	return blessEntry, nil
}
