// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
)

// IMPORTANT:
// It's essential that the ctx not be accessed when authentication
// is not requested. This is because the context initialization code
// uses an unauthenticated connection to obtain a principal!

func GetPrincipalVCOpts(ctx *context.T, opts ...VCOpt) security.Principal {
	for _, opt := range opts {
		switch v := opt.(type) {
		case AuthenticatedVC:
			if bool(v) == false {
				return nil
			}
			return v23.GetPrincipal(ctx)
		}
	}
	return v23.GetPrincipal(ctx)
}

func GetPrincipalListenerOpts(ctx *context.T, opts ...ListenerOpt) security.Principal {
	for _, opt := range opts {
		switch v := opt.(type) {
		case AuthenticatedVC:
			if bool(v) == false {
				return nil
			}
			return v23.GetPrincipal(ctx)
		}
	}
	return v23.GetPrincipal(ctx)
}
