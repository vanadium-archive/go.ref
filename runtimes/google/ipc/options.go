package ipc

import (
	"time"

	"v.io/core/veyron/runtimes/google/ipc/stream"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
)

// PreferredProtocols instructs the Runtime implementation to select
// endpoints with the specified protocols when a Client makes a call
// and to order them in the specified order.
type PreferredProtocols []string

func (PreferredProtocols) IPCClientOpt() {}

// This option is used to sort and filter the endpoints when resolving the
// proxy name from a mounttable.
type PreferredServerResolveProtocols []string

func (PreferredServerResolveProtocols) IPCServerOpt() {}

// ReservedNameDispatcher specifies the dispatcher that controls access
// to framework managed portion of the namespace.
type ReservedNameDispatcher struct {
	Dispatcher ipc.Dispatcher
}

func (ReservedNameDispatcher) IPCServerOpt() {}

func getRetryTimeoutOpt(opts []ipc.CallOpt) (time.Duration, bool) {
	for _, o := range opts {
		if r, ok := o.(options.RetryTimeout); ok {
			return time.Duration(r), true
		}
	}
	return 0, false
}

func getNoResolveOpt(opts []ipc.CallOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.NoResolve); ok {
			return true
		}
	}
	return false
}

func shouldNotFetchDischarges(opts []ipc.CallOpt) bool {
	for _, o := range opts {
		if _, ok := o.(NoDischarges); ok {
			return true
		}
	}
	return false
}

func getNoRetryOpt(opts []ipc.CallOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.NoRetry); ok {
			return true
		}
	}
	return false
}

func getVCOpts(opts []ipc.CallOpt) (vcOpts []stream.VCOpt) {
	for _, o := range opts {
		if v, ok := o.(stream.VCOpt); ok {
			vcOpts = append(vcOpts, v)
		}
	}
	return
}

func getResolveOpts(opts []ipc.CallOpt) (resolveOpts []naming.ResolveOpt) {
	for _, o := range opts {
		if r, ok := o.(naming.ResolveOpt); ok {
			resolveOpts = append(resolveOpts, r)
		}
	}
	return
}

func vcEncrypted(vcOpts []stream.VCOpt) bool {
	encrypted := true
	for _, o := range vcOpts {
		switch o {
		case options.VCSecurityNone:
			encrypted = false
		case options.VCSecurityConfidential:
			encrypted = true
		}
	}
	return encrypted
}
