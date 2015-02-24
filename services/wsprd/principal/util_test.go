package principal

import (
	"fmt"
	"strings"

	vsecurity "v.io/core/veyron/security"

	"v.io/v23/security"
	"v.io/v23/verror"
)

func newPrincipal() security.Principal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	return p
}

func blessSelf(p security.Principal, name string) security.Blessings {
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return b
}

func matchesError(got error, want string) error {
	if (got == nil) && len(want) == 0 {
		return nil
	}
	if got == nil {
		return fmt.Errorf("Got nil error, wanted to match %q", want)
	}
	if !strings.Contains(got.Error(), want) {
		return fmt.Errorf("Got error %q, wanted to match %q", got, want)
	}
	return nil
}

func matchesErrorID(got error, want verror.ID) error {
	if (got == nil) && len(want) == 0 {
		return nil
	}
	if got == nil {
		return fmt.Errorf("Got nil error, wanted to match %q", want)
	}
	if !verror.Is(got, want) {
		return fmt.Errorf("Got error %q, wanted to match %q", got, want)
	}
	return nil
}
