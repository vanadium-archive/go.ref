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
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref"
	"v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/mgmt"
	vsecurity "v.io/x/ref/lib/security"
	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/services/agent/agentlib"
)

func (r *Runtime) initPrincipal(ctx *context.T, credentials string) (principal security.Principal, deps []interface{}, shutdown func(), err error) {
	if principal, _ = ctx.Value(principalKey).(security.Principal); principal != nil {
		return principal, nil, func() {}, nil
	}
	if len(credentials) > 0 {
		// Explicitly specified credentials, ignore the agent.
		if _, fd, _ := agentEP(); fd >= 0 {
			syscall.Close(fd)
		}
		// TODO(ataly, ashankar): If multiple runtimes are getting
		// initialized at the same time from the same
		// ref.EnvCredentials we will need some kind of locking for the
		// credential files.
		if principal, err = vsecurity.LoadPersistentPrincipal(credentials, nil); err != nil {
			if os.IsNotExist(err) {
				if principal, err = vsecurity.CreatePersistentPrincipal(credentials, nil); err != nil {
					return principal, nil, nil, err
				}
				return principal, nil, func() {}, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
			}
			return nil, nil, nil, err
		}
		return principal, nil, func() {}, nil
	}
	// Use credentials stored in the agent.
	if ep, _, err := agentEP(); err != nil {
		return nil, nil, nil, err
	} else if ep != nil {
		// Use a new stream manager and an "incomplete" client (the
		// principal is nil) to talk to the agent.
		//
		// The lack of a principal works out for the rpc.Client
		// only because the agent uses anonymous unix sockets and
		// the SecurityNone option.
		//
		// Using a distinct stream manager to manage agent-related
		// connections helps isolate these connections to the agent
		// from management of any other connections created in the
		// process (such as future RPCs to other services).
		if ctx, err = r.WithNewStreamManager(ctx); err != nil {
			return nil, nil, nil, err
		}
		client := r.GetClient(ctx)

		// We reparent the context we use to construct the agent.
		// We do this because the agent needs to be able to make RPCs
		// during runtime shutdown.
		ctx, shutdown = context.WithRootCancel(ctx)

		// TODO(cnicolaou): the agentlib can call back into runtime to get the principal,
		// which will be a problem if the runtime is not initialized, hence this code
		// path is fragile. We should ideally provide an option to work around this case.
		if principal, err = agentlib.NewAgentPrincipal(ctx, ep, client); err != nil {
			shutdown()
			client.Close()
			return nil, nil, nil, err
		}
		return principal, []interface{}{client}, shutdown, nil
	}
	// No agent, no explicit credentials specified: - create a new principal and blessing in memory.
	if principal, err = vsecurity.NewPrincipal(); err != nil {
		return principal, nil, nil, err
	}
	return principal, nil, func() {}, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
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
		endpoint = os.Getenv(ref.EnvAgentEndpoint)
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
