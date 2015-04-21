// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref/envvar"
	"v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/mgmt"
	vsecurity "v.io/x/ref/lib/security"
	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/services/agent/agentlib"
)

func initSecurity(ctx *context.T, credentials string, client rpc.Client) (security.Principal, error) {
	principal, err := setupPrincipal(ctx, credentials, client)
	if err != nil {
		return nil, err
	}

	return principal, nil
}

func setupPrincipal(ctx *context.T, credentials string, client rpc.Client) (security.Principal, error) {
	var err error
	var principal security.Principal
	if principal, _ = ctx.Value(principalKey).(security.Principal); principal != nil {
		return principal, nil
	}
	if len(credentials) > 0 {
		// We close the agentFD if that is also provided
		if _, fd, _ := agentEP(); fd >= 0 {
			syscall.Close(fd)
		}
		// TODO(ataly, ashankar): If multiple runtimes are getting
		// initialized at the same time from the same
		// envvar.Credentials we will need some kind of locking for the
		// credential files.
		if principal, err = vsecurity.LoadPersistentPrincipal(credentials, nil); err != nil {
			if os.IsNotExist(err) {
				if principal, err = vsecurity.CreatePersistentPrincipal(credentials, nil); err != nil {
					return principal, err
				}
				return principal, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
			}
			return nil, err
		}
		return principal, nil
	}
	if ep, _, err := agentEP(); err != nil {
		return nil, err
	} else if ep != nil {
		return agentlib.NewAgentPrincipal(ctx, ep, client)
	}
	if principal, err = vsecurity.NewPrincipal(); err != nil {
		return principal, err
	}
	return principal, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
}

func parseAgentFD(ep naming.Endpoint) (int, error) {
	fd := ep.Addr().String()
	ifd, err := strconv.Atoi(fd)
	if err != nil {
		ifd = -1
	}
	return ifd, nil
}

// agentEP returns an Endpoint to be used to communicate with
// the security agent if the current process has been configured to use the
// agent.
func agentEP() (naming.Endpoint, int, error) {
	handle, err := exec.GetChildHandle()
	if err != nil && verror.ErrorID(err) != exec.ErrNoVersion.ID {
		return nil, -1, err
	}
	var endpoint string
	if handle != nil {
		// We were started by a parent (presumably, device manager).
		endpoint, _ = handle.Config.Get(mgmt.SecurityAgentEndpointConfigKey)
	} else {
		endpoint = os.Getenv(envvar.AgentEndpoint)
	}
	if endpoint == "" {
		return nil, -1, nil
	}
	ep, err := inaming.NewEndpoint(endpoint)
	if err != nil {
		return nil, -1, err
	}

	// Don't let children accidentally inherit the agent connection.
	fd, err := parseAgentFD(ep)
	if err != nil {
		return nil, -1, err
	}
	if fd >= 0 {
		syscall.CloseOnExec(fd)
	}
	return ep, fd, nil
}

func defaultBlessingName() string {
	var name string
	if user, _ := user.Current(); user != nil && len(user.Username) > 0 {
		name = user.Username
	} else {
		name = "anonymous"
	}
	if host, _ := os.Hostname(); len(host) > 0 {
		name = name + "@" + host
	}
	return fmt.Sprintf("%s-%d", name, os.Getpid())
}
