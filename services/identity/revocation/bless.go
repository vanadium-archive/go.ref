package revocation

import (
	"fmt"
	"time"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/audit"

	"veyron.io/veyron/veyron2/security"
)

// Bless creates a blessing on behalf of the identity server.
func Bless(server security.PrivateID, blessee security.PublicID, email string, duration time.Duration, caveats []security.Caveat, revocationCaveat security.ThirdPartyCaveat) (security.PublicID, error) {
	if revocationCaveat != nil {
		caveat, err := security.NewCaveat(revocationCaveat)
		if err != nil {
			return nil, err
		}
		// revocationCaveat must be prepended because it is assumed to be first by ReadBlessAuditEntry.
		caveats = append([]security.Caveat{caveat}, caveats...)
	}
	// TODO(suharshs): Extend the duration for blessings with provided revocaionCaveats.
	return server.Bless(blessee, email, duration, caveats)
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
	caveats, _ := entry.Arguments[3].([]security.Caveat)
	if len(caveats) > 0 {
		revocationCaveat, err := vsecurity.CaveatValidators(caveats[0])
		if err != nil {
			return blessEntry, err
		}
		blessEntry.RevocationCaveat, _ = revocationCaveat[0].(security.ThirdPartyCaveat)
	}
	return blessEntry, nil
}
