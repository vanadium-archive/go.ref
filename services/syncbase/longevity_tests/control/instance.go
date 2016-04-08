// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package control

import (
	"fmt"
	"os"

	"v.io/x/lib/gosh"
	"v.io/x/ref/services/syncbase/longevity_tests/model"
	"v.io/x/ref/services/syncbase/longevity_tests/syncbased_vine"
	"v.io/x/ref/services/syncbase/syncbaselib"
)

var (
	syncbasedMain = gosh.RegisterFunc("syncbasedMain", syncbased_vine.Main)
)

// instance encapsulates a running syncbase instance.
type instance struct {
	// Name of the instance.  Syncbased will be mounted under this name.
	name string

	// Gosh command for syncbase process.  Will be nil if instance is not
	// running.
	cmd *gosh.Cmd

	// Directory containing v23 credentials for this instance.
	credsDir string

	// Namespace root.
	namespaceRoot string

	// Gosh shell used to spawn all processes.
	sh *gosh.Shell

	// Name of the vine server and tag running on instance.
	vineName string

	// Working directory for this instance.
	wd string
}

func (inst *instance) start() error {
	if inst.cmd != nil {
		return fmt.Errorf("inst %v already started", inst)
	}
	opts := syncbaselib.Opts{
		Name:    inst.name,
		RootDir: inst.wd,
		// TODO(nlacasse): Turn this on once neighborhoods can be configured via VINE.
		SkipPublishInNh: false,
	}
	// We use the vine name as both the vine server name and the tag.
	inst.cmd = inst.sh.FuncCmd(syncbasedMain, inst.vineName, inst.vineName, opts)
	if inst.sh.Err != nil {
		return inst.sh.Err
	}
	// TODO(nlacasse): Once we have user credentials, only allow blessings
	// based on the user here.
	perms := `{"Admin":{"In":["root"],"NotIn":null},"Debug":{"In":["root"],"NotIn":null},"Read":{"In":["root"],"NotIn":null},"Resolve":{"In":["root"],"NotIn":null},"Write":{"In":["root"],"NotIn":null}}`
	inst.cmd.Args = append(inst.cmd.Args,
		"--log_dir="+inst.wd,
		"--v23.namespace.root="+inst.namespaceRoot,
		"--v23.credentials="+inst.credsDir,
		"--v23.permissions.literal="+perms,
		//"--vmodule=*=2",
	)
	if inst.cmd.Start(); inst.cmd.Err != nil {
		return inst.cmd.Err
	}
	vars := inst.cmd.AwaitVars("ENDPOINT")
	if ep := vars["ENDPOINT"]; ep == "" {
		return fmt.Errorf("error starting %q: no ENDPOINT variable sent from process", inst.name)
	}
	return nil
}

func (i *instance) update(d *model.Device) error {
	return fmt.Errorf("not implemented")
}

func (i *instance) stop() error {
	i.cmd.Terminate(os.Interrupt)
	if i.cmd.Err != nil {
		return i.cmd.Err
	}
	i.cmd = nil
	return nil
}

func (i *instance) kill() error {
	i.cmd.Terminate(os.Kill)
	if i.cmd.Err != nil {
		return i.cmd.Err
	}
	i.cmd = nil
	return nil
}
