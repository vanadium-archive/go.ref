package audit

import (
	"fmt"
	"time"

	"veyron2/security"
)

type auditingID struct {
	id      security.PrivateID
	auditor Auditor
}

type args []interface{}

// NewPrivateID returns a security.PrivateID implementation that wraps over 'wrapped' but
// logs all operations that use the private key of wrapped to the auditor.
func NewPrivateID(wrapped security.PrivateID, auditor Auditor) security.PrivateID {
	return &auditingID{wrapped, auditor}
}

func (id *auditingID) Sign(message []byte) (security.Signature, error) {
	sig, err := id.id.Sign(message)
	// Do not save the signature itself.
	if err = id.audit(err, "Sign", args{message}, nil); err != nil {
		return security.Signature{}, err
	}
	return sig, nil
}

func (id *auditingID) PublicKey() security.PublicKey {
	return id.id.PublicKey()
}

func (id *auditingID) PublicID() security.PublicID {
	return id.id.PublicID()
}

func (id *auditingID) Bless(blessee security.PublicID, blessingName string, duration time.Duration, caveats []security.ServiceCaveat) (security.PublicID, error) {
	blessed, err := id.id.Bless(blessee, blessingName, duration, caveats)
	if err = id.audit(err, "Bless", args{blessee, blessingName, duration, caveats}, blessed); err != nil {
		return nil, err
	}
	return blessed, nil
}

func (id *auditingID) Derive(publicID security.PublicID) (security.PrivateID, error) {
	// No auditing for Derive. Two reasons:
	// (1) This method is expected to go away (ataly@)
	// (2) There is no operation on the private key here.
	// However, operations on the Derived id do need to be audited.
	derived, err := id.id.Derive(publicID)
	if err != nil {
		return nil, err
	}
	return NewPrivateID(derived, id.auditor), nil
}

func (id *auditingID) MintDischarge(caveat security.ThirdPartyCaveat, context security.Context, duration time.Duration, caveats []security.ServiceCaveat) (security.ThirdPartyDischarge, error) {
	d, err := id.id.MintDischarge(caveat, context, duration, caveats)
	if err = id.audit(err, "MintDischarge", args{caveat, context, duration, caveats}, nil); err != nil {
		return nil, err
	}
	return d, nil
}

func (id *auditingID) audit(err error, method string, args args, result interface{}) error {
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
	if err := id.auditor.Audit(entry); err != nil {
		return fmt.Errorf("failed to audit call to %q: %v", method, err)
	}
	return nil
}
