package security

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"veyron2/security"
	"veyron2/vom"
)

type setPublicID []security.PublicID

var (
	errEmptySet         = errors.New("suspected manipulation of identity: empty set")
	errSingleElementSet = errors.New("suspected manipulation of identity: single element set")
	errNoNilsInSet      = errors.New("suspected manipulation of identity: nil element in set")
	errMismatchedKeys   = errors.New("mismatched keys in elements of set")
)

// NewSetPublicID returns a security.PublicID containing the combined credentials of ids.
// It requires that all elements of ids have the same public key.
func NewSetPublicID(ids ...security.PublicID) (security.PublicID, error) {
	// All public keys must match
	switch len(ids) {
	case 0:
		return nil, nil
	case 1:
		return ids[0], nil
	default:
		for i := 1; i < len(ids); i++ {
			if !reflect.DeepEqual(ids[0].PublicKey(), ids[i].PublicKey()) {
				return nil, errMismatchedKeys
			}
		}
		set := setPublicID(ids)
		return &set, nil
	}
}

func (s *setPublicID) Names() []string {
	names := make([]string, 0, len(*s))
	for _, id := range *s {
		names = append(names, id.Names()...)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (s *setPublicID) PublicKey() security.PublicKey {
	return (*s)[0].PublicKey()
}

func (s *setPublicID) Authorize(context security.Context) (security.PublicID, error) {
	var authids []security.PublicID
	var errs []error
	for _, id := range *s {
		if aid, err := id.Authorize(context); err != nil && len(authids) == 0 {
			errs = append(errs, err)
		} else if aid != nil {
			authids = append(authids, aid)
		}
	}
	if len(authids) == 0 {
		return nil, joinerrs(errs)
	}
	return NewSetPublicID(authids...)
}

func (s *setPublicID) ThirdPartyCaveats() []security.ThirdPartyCaveat {
	set := make(map[string]security.ThirdPartyCaveat)
	for _, id := range *s {
		for _, tp := range id.ThirdPartyCaveats() {
			set[tp.ID()] = tp
		}
	}
	if len(set) == 0 {
		return nil
	}
	ret := make([]security.ThirdPartyCaveat, 0, len(set))
	for _, c := range set {
		ret = append(ret, c)
	}
	return ret
}

func (s *setPublicID) String() string {
	strs := make([]string, len(*s))
	for ix, id := range *s {
		strs[ix] = fmt.Sprintf("%v", id)
	}
	return strings.Join(strs, "#")
}

func (s *setPublicID) VomEncode() ([]security.PublicID, error) {
	return []security.PublicID(*s), nil
}

func (s *setPublicID) VomDecode(ids []security.PublicID) error {
	switch len(ids) {
	case 0:
		return errEmptySet
	case 1:
		return errSingleElementSet
	}
	if ids[0] == nil {
		return errNoNilsInSet
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] == nil {
			return errNoNilsInSet
		}
		if !reflect.DeepEqual(ids[0].PublicKey(), ids[i].PublicKey()) {
			return errMismatchedKeys
		}
	}
	*s = setPublicID(ids)
	return nil
}

type setPrivateID []security.PrivateID

// NewSetPrivateID returns a security.PrivateID contiaining the combined credentials of ids.
// It requires that all ids have the same private key.
func NewSetPrivateID(ids ...security.PrivateID) (security.PrivateID, error) {
	switch len(ids) {
	case 0:
		return nil, nil
	case 1:
		return ids[0], nil
	default:
		pub := ids[0].PublicID().PublicKey()
		for i := 1; i < len(ids); i++ {
			if !reflect.DeepEqual(pub, ids[i].PublicID().PublicKey()) {
				return nil, errMismatchedKeys
			}
		}
		return setPrivateID(ids), nil
	}
}

func (s setPrivateID) PublicID() security.PublicID {
	pubs := make([]security.PublicID, len(s))
	for ix, id := range s {
		pubs[ix] = id.PublicID()
	}
	set := setPublicID(pubs)
	return &set
}

func (s setPrivateID) Sign(message []byte) (security.Signature, error) { return s[0].Sign(message) }

func (s setPrivateID) PublicKey() security.PublicKey { return s[0].PublicKey() }

func (s setPrivateID) Bless(blessee security.PublicID, blessingName string, duration time.Duration, caveats []security.Caveat) (security.PublicID, error) {
	pubs := make([]security.PublicID, len(s))
	for ix, id := range s {
		var err error
		if pubs[ix], err = id.Bless(blessee, blessingName, duration, caveats); err != nil {
			return nil, err
		}
	}
	return NewSetPublicID(pubs...)
}

func (s setPrivateID) Derive(pub security.PublicID) (security.PrivateID, error) {
	switch p := pub.(type) {
	case *chainPublicID:
		return s[0].Derive(p)
	case *setPublicID:
		privs := make([]security.PrivateID, len(*p))
		var err error
		for ix, ip := range *p {
			if privs[ix], err = s.Derive(ip); err != nil {
				return nil, fmt.Errorf("Derive failed for %d of %d id in set", ix, len(*p))
			}
		}
		return setPrivateID(privs), nil
	default:
		return nil, fmt.Errorf("PrivateID of type %T cannot be used to Derive from PublicID of type %T", s, pub)
	}
}

func (s setPrivateID) MintDischarge(cav security.ThirdPartyCaveat, ctx security.Context, duration time.Duration, dischargeCaveats []security.Caveat) (security.Discharge, error) {
	for _, id := range s {
		if d, err := id.MintDischarge(cav, ctx, duration, dischargeCaveats); err == nil {
			return d, nil
		}
	}
	return nil, fmt.Errorf("discharge cannot be constructed for %T from %T", cav, s)
}

func joinerrs(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	strs := make([]string, len(errs))
	for i, err := range errs {
		strs[i] = err.Error()
	}
	return fmt.Errorf("none of the blessings in the set are authorized: %v", strings.Join(strs, ", "))
}

func init() {
	vom.Register(setPrivateID(nil))
	vom.Register(setPublicID(nil))
}
