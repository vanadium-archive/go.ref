package audit

import (
	"fmt"
	"time"

	"v.io/core/veyron2/security"
)

// NewPrincipal returns a security.Principal implementation that logs
// all private key operations of 'wrapped' to 'auditor' (i.e., all calls to
// BlessSelf, Bless, MintDischarge and Sign).
func NewPrincipal(wrapped security.Principal, auditor Auditor) security.Principal {
	return &auditingPrincipal{wrapped, auditor}
}

type auditingPrincipal struct {
	principal security.Principal
	auditor   Auditor
}

type args []interface{}

func (p *auditingPrincipal) Bless(key security.PublicKey, with security.Blessings, extension string, caveat security.Caveat, additionalCaveats ...security.Caveat) (security.Blessings, error) {
	blessings, err := p.principal.Bless(key, with, extension, caveat, additionalCaveats...)
	if err = p.audit(err, "Bless", addCaveats(args{key, with, extension, caveat}, additionalCaveats...), blessings); err != nil {
		return nil, err
	}
	return blessings, nil
}

func (p *auditingPrincipal) BlessSelf(name string, caveats ...security.Caveat) (security.Blessings, error) {
	blessings, err := p.principal.BlessSelf(name, caveats...)
	if err = p.audit(err, "BlessSelf", addCaveats(args{name}, caveats...), blessings); err != nil {
		return nil, err
	}
	return blessings, nil
}

func (p *auditingPrincipal) Sign(message []byte) (security.Signature, error) {
	// Do not save the signature itself.
	sig, err := p.principal.Sign(message)
	if err = p.audit(err, "Sign", args{message}, nil); err != nil {
		return security.Signature{}, err
	}
	return sig, nil
}

func (p *auditingPrincipal) MintDischarge(tp security.ThirdPartyCaveat, caveat security.Caveat, additionalCaveats ...security.Caveat) (security.Discharge, error) {
	d, err := p.principal.MintDischarge(tp, caveat, additionalCaveats...)
	// No need to log the discharge
	if err = p.audit(err, "MintDischarge", addCaveats(args{tp, caveat}, additionalCaveats...), nil); err != nil {
		return nil, err
	}
	return d, nil
}

func (p *auditingPrincipal) BlessingsByName(name security.BlessingPattern) []security.Blessings {
	return p.principal.BlessingsByName(name)
}

func (p *auditingPrincipal) BlessingsInfo(b security.Blessings) map[string][]security.Caveat {
	return p.principal.BlessingsInfo(b)
}

func (p *auditingPrincipal) PublicKey() security.PublicKey         { return p.principal.PublicKey() }
func (p *auditingPrincipal) Roots() security.BlessingRoots         { return p.principal.Roots() }
func (p *auditingPrincipal) BlessingStore() security.BlessingStore { return p.principal.BlessingStore() }
func (p *auditingPrincipal) AddToRoots(b security.Blessings) error { return p.principal.AddToRoots(b) }

func (p *auditingPrincipal) audit(err error, method string, args args, result interface{}) error {
	if err != nil {
		return err
	}
	entry := Entry{Method: method, Timestamp: time.Now()}
	if len(args) > 0 {
		entry.Arguments = []interface{}(args)
	}
	if result != nil {
		entry.Results = []interface{}{result}
	}
	if err := p.auditor.Audit(entry); err != nil {
		return fmt.Errorf("failed to audit call to %q: %v", method, err)
	}
	return nil
}

func addCaveats(args args, caveats ...security.Caveat) args {
	for _, c := range caveats {
		// TODO(ashankar,suharshs): Should isUnconstrainedCaveat in veyron2/security be exported and used here?
		if len(c.ValidatorVOM) > 0 {
			args = append(args, c)
		}
	}
	return args
}
