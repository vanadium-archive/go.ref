package blesser

import (
	"bytes"
	"fmt"
	"time"

	"veyron.io/veyron/veyron/services/identity"
	"veyron.io/veyron/veyron/services/identity/util"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vom"
)

type macaroonBlesser struct {
	key []byte
}

// BlessingMacaroon contains the data that is encoded into the macaroon for creating blessings.
type BlessingMacaroon struct {
	Creation time.Time
	Caveats  []security.Caveat
	Name     string
}

// NewMacaroonBlesserServer provides an identity.MacaroonBlesser Service that generates blessings
// after unpacking a BlessingMacaroon.
func NewMacaroonBlesserServer(key []byte) identity.MacaroonBlesserServerStub {
	return identity.MacaroonBlesserServer(&macaroonBlesser{key})
}

func (b *macaroonBlesser) Bless(ctx ipc.ServerContext, macaroon string) (security.WireBlessings, error) {
	var empty security.WireBlessings
	inputs, err := util.Macaroon(macaroon).Decode(b.key)
	if err != nil {
		return empty, err
	}
	var m BlessingMacaroon
	if err := vom.NewDecoder(bytes.NewBuffer(inputs)).Decode(&m); err != nil {
		return empty, err
	}
	if time.Now().After(m.Creation.Add(time.Minute * 5)) {
		return empty, fmt.Errorf("macaroon has expired")
	}
	if ctx.LocalPrincipal() == nil {
		return empty, fmt.Errorf("server misconfiguration: no authentication happened")
	}
	if len(m.Caveats) == 0 {
		m.Caveats = []security.Caveat{security.UnconstrainedUse()}
	}
	// TODO(ashankar,toddw): After the VDL configuration files have the
	// scheme to translate between "wire" types and "in-memory" types, this
	// should just become return ctx.LocalPrincipal().....
	blessings, err := ctx.LocalPrincipal().Bless(ctx.RemoteBlessings().PublicKey(), ctx.LocalBlessings(), m.Name, m.Caveats[0], m.Caveats[1:]...)
	if err != nil {
		return empty, err
	}
	return security.MarshalBlessings(blessings), nil
}
