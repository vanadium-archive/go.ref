// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt

import (
	"fmt"
	"os"
	"os/user"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/ref"
	"v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/mgmt"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/agentlib"
)

func (r *Runtime) initPrincipal(ctx *context.T, credentials string) (principal security.Principal, deps []interface{}, shutdown func(), err error) {
	if principal, _ = ctx.Value(principalKey).(security.Principal); principal != nil {
		return principal, nil, func() {}, nil
	}
	if len(credentials) > 0 {
		// Explicitly specified credentials, ignore the agent.

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
	if principal, err := ipcAgent(); err != nil {
		return nil, nil, nil, err
	} else if principal != nil {
		return principal, nil, func() { principal.Close() }, nil
	}
	// No agent, no explicit credentials specified: - create a new principal and blessing in memory.
	if principal, err = vsecurity.NewPrincipal(); err != nil {
		return principal, nil, nil, err
	}
	return principal, nil, func() {}, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
}

func ipcAgent() (agent.Principal, error) {
	var config exec.Config
	config, err := exec.ReadConfigFromOSEnv()
	if config == nil || err != nil {
		// TODO(cnicolaou): backwards compatibility,
		// remove when binaries are pushed to prod and replace with
		// if err != nil {
		//     return nil, err
		// }
		handle, err := exec.GetChildHandle()
		if err != nil && verror.ErrorID(err) != exec.ErrNoVersion.ID {
			return nil, err
		}
		if handle != nil {
			config = handle.Config
		}
	}
	var path string
	if config != nil {
		// We were started by a parent (presumably, device manager).
		path, _ = config.Get(mgmt.SecurityAgentPathConfigKey)
	} else {
		path = os.Getenv(ref.EnvAgentPath)
	}
	if path == "" {
		return nil, nil
	}
	return agentlib.NewAgentPrincipalX(path)
}

func defaultBlessingName() string {
	var name string
	if user, _ := user.Current(); user != nil && len(user.Username) > 0 {
		name = user.Username
	} else {
		name = "anonymous"
	}
	host, _ := os.Hostname()
	if host == "(none)" {
		// (none) is a common default hostname and contains parentheses,
		// which are invalid blessings characters.
		host = "anonymous"
	}
	if len(host) > 0 {
		name = name + "@" + host
	}
	return fmt.Sprintf("%s-%d", name, os.Getpid())
}
