// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"time"

	"v.io/x/ref/profiles/internal/rpc/stream"

	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
)

// PreferredProtocols instructs the Runtime implementation to select
// endpoints with the specified protocols when a Client makes a call
// and to order them in the specified order.
type PreferredProtocols []string

func (PreferredProtocols) RPCClientOpt() {}

// This option is used to sort and filter the endpoints when resolving the
// proxy name from a mounttable.
type PreferredServerResolveProtocols []string

func (PreferredServerResolveProtocols) RPCServerOpt() {}

// ReservedNameDispatcher specifies the dispatcher that controls access
// to framework managed portion of the namespace.
type ReservedNameDispatcher struct {
	Dispatcher rpc.Dispatcher
}

func (ReservedNameDispatcher) RPCServerOpt() {}

func getRetryTimeoutOpt(opts []rpc.CallOpt) (time.Duration, bool) {
	for _, o := range opts {
		if r, ok := o.(options.RetryTimeout); ok {
			return time.Duration(r), true
		}
	}
	return 0, false
}

func getNoNamespaceOpt(opts []rpc.CallOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.NoResolve); ok {
			return true
		}
	}
	return false
}

func shouldNotFetchDischarges(opts []rpc.CallOpt) bool {
	for _, o := range opts {
		if _, ok := o.(NoDischarges); ok {
			return true
		}
	}
	return false
}

func noRetry(opts []rpc.CallOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.NoRetry); ok {
			return true
		}
	}
	return false
}

func getVCOpts(opts []rpc.CallOpt) (vcOpts []stream.VCOpt) {
	for _, o := range opts {
		if v, ok := o.(stream.VCOpt); ok {
			vcOpts = append(vcOpts, v)
		}
	}
	return
}

func getNamespaceOpts(opts []rpc.CallOpt) (resolveOpts []naming.NamespaceOpt) {
	for _, o := range opts {
		if r, ok := o.(naming.NamespaceOpt); ok {
			resolveOpts = append(resolveOpts, r)
		}
	}
	return
}

func callEncrypted(opts []rpc.CallOpt) bool {
	encrypted := true
	for _, o := range opts {
		switch o {
		case options.SecurityNone:
			encrypted = false
		case options.SecurityConfidential:
			encrypted = true
		}
	}
	return encrypted
}
