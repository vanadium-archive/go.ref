// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import "fmt"

type CloudVM interface {
	// Name of the VM instance that the object talks to
	Name() string

	// IP address (as a string) of the VM instance
	IP() string

	// Execute a command on the VM instance. The current directory will be the
	// working directory of the VM when the command is run.
	RunCommand(...string) (output []byte, err error)

	// Copy a file to the working directory of VM instance. The destination is treated as a
	// pathname relative to the workspace of the VM.
	CopyFile(infile, destination string) error

	// Delete the VM instance
	Delete() error

	// Provide what the user must run to run a specified command on the VM.
	RunCommandForUser(commandPlusArgs ...string) string

	// Provide the command that the user can use to delete a VM instance for which Delete()
	// was not called
	DeleteCommandForUser() string
}

func CreateCloudVM(instanceName string, options interface{}) (CloudVM, error) {
	switch t := options.(type) {
	default:
		return nil, fmt.Errorf("Unknown options type")
	case VcloudVMOptions:
		return newVcloudVM(instanceName, t)
	case SSHVMOptions:
		return newSSHVM(instanceName, t)
	}
}
