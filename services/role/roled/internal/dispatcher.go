// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"

	"v.io/x/ref/services/discharger"
	"v.io/x/ref/services/role"

	"v.io/x/lib/vlog"
)

const requiredSuffix = security.ChainSeparator + role.RoleSuffix

// NewDispatcher returns a dispatcher object for a role service and its
// associated discharger service.
// The configRoot is the top level directory where the role configuration files
// are stored.
// The dischargerLocation is the object name or address of the discharger
// service for the third-party caveats attached to the role blessings returned
// by the role service.
func NewDispatcher(configRoot, dischargerLocation string) rpc.Dispatcher {
	return &dispatcher{configRoot, dischargerLocation}
}

type dispatcher struct {
	configRoot         string
	dischargerLocation string
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if len(suffix) == 0 {
		return discharger.DischargerServer(&dischargerImpl{}), &openAuthorizer{}, nil
	}
	fileName := filepath.Join(d.configRoot, filepath.FromSlash(suffix+".conf"))
	if !strings.HasPrefix(fileName, d.configRoot) {
		// Guard against ".." in the suffix that could be used to read
		// files outside of the config root.
		return nil, nil, verror.New(verror.ErrNoExistOrNoAccess, nil)
	}
	config, err := loadExpandedConfig(fileName, nil)
	if err != nil && !os.IsNotExist(err) {
		// The config file exists, but we failed to read it for some
		// reason. This is likely a server configuration error.
		vlog.Errorf("loadConfig(%q, %q): %v", d.configRoot, suffix, err)
		return nil, nil, verror.Convert(verror.ErrInternal, nil, err)
	}
	obj := &roleService{role: suffix, config: config, dischargerLocation: d.dischargerLocation}
	return role.RoleServer(obj), &authorizer{config}, nil
}

type openAuthorizer struct{}

func (openAuthorizer) Authorize(*context.T) error {
	return nil
}

type authorizer struct {
	config *Config
}

func (a *authorizer) Authorize(ctx *context.T) error {
	if a.config == nil {
		return verror.New(verror.ErrNoExistOrNoAccess, ctx)
	}
	remoteBlessingNames, _ := security.RemoteBlessingNames(ctx)

	for _, pattern := range a.config.Members {
		if pattern.MatchedBy(remoteBlessingNames...) {
			return nil
		}
	}
	return verror.New(verror.ErrNoExistOrNoAccess, ctx)
}

func loadExpandedConfig(fileName string, seenFiles map[string]struct{}) (*Config, error) {
	if seenFiles == nil {
		seenFiles = make(map[string]struct{})
	}
	if _, seen := seenFiles[fileName]; seen {
		return nil, nil
	}
	seenFiles[fileName] = struct{}{}
	c, err := loadConfig(fileName)
	if err != nil {
		return nil, err
	}
	parentDir := filepath.Dir(fileName)
	for _, imp := range c.ImportMembers {
		f := filepath.Join(parentDir, filepath.FromSlash(imp+".conf"))
		ic, err := loadExpandedConfig(f, seenFiles)
		if err != nil {
			vlog.Errorf("loadExpandedConfig(%q) failed: %v", f, err)
			continue
		}
		if ic == nil {
			continue
		}
		c.Members = append(c.Members, ic.Members...)
	}
	c.ImportMembers = nil
	dedupMembers(c)
	return c, nil
}

func loadConfig(fileName string) (*Config, error) {
	contents, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(contents, &c); err != nil {
		return nil, err
	}
	for i, pattern := range c.Members {
		if p := string(pattern); !strings.HasSuffix(p, requiredSuffix) {
			c.Members[i] = security.BlessingPattern(p + requiredSuffix)
		}
	}
	return &c, nil
}

func dedupMembers(c *Config) {
	members := make(map[security.BlessingPattern]struct{})
	for _, m := range c.Members {
		members[m] = struct{}{}
	}
	c.Members = []security.BlessingPattern{}
	for m := range members {
		c.Members = append(c.Members, m)
	}
}
