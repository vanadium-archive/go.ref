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

	// Execute a command on the VM instance
	RunCommand(...string) (output []byte, err error)

	// Copy a file to the VM instance
	CopyFile(infile, destination string) error

	// Delete the VM instance
	Delete() error

	// Provide the command that the user can use to delete a VM instance for which Delete()
	// was not called
	DeleteCommandForUser() string
}

func CreateCloudVM(instanceName string, options interface{}) (CloudVM, error) {
	switch options.(type) {
	default:
		return nil, fmt.Errorf("Unknown options type")
	case VcloudVMOptions:
		return newVcloudVM(instanceName, options.(VcloudVMOptions))
	}
}
