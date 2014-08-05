// Package auditor provides mechanisms to write method invocations to an audit log.
//
// Typical use would be for tracking sensitive operations like private key usage (NewPrivateID),
// or sensitive RPC method invocations.
package auditor

import "time"

// Auditor is the interface for writing auditable events.
type Auditor interface {
	Audit(entry Entry) error
}

// Entry is the information logged on each auditable event.
type Entry struct {
	// Method being invoked.
	Method string
	// Arguments to the method.
	// Any sensitive data in the arguments should not be included,
	// even if the argument was provided to the real method invocation.
	Arguments []interface{}
	// Result of the method invocation.
	// A common use case is to audit only successful method invocations.
	Results []interface{}

	// Timestamp of method invocation.
	Timestamp time.Time
}
