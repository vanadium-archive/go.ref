// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package exec

import (
	"log"
	"os/exec"
	"time"
)

func ExampleChildHandle() {
	ch, _ := GetChildHandle()
	// Initalize the app/service, access the secret shared with the
	// child by its parent
	_ = ch.Secret
	ch.SetReady()
	// Do work
}

func ExampleParentHandle() {
	cmd := exec.Command("/bin/hostname")
	ph := NewParentHandle(cmd, SecretOpt("secret"))

	// Start the child process.
	if err := ph.Start(); err != nil {
		log.Printf("failed to start child: %s\n", err)
		return
	}

	// Wait for the child to become ready.
	if err := ph.WaitForReady(time.Second); err != nil {
		log.Printf("failed to start child: %s\n", err)
		return
	}

	// Wait for the child to exit giving it an hour to do it's work.
	if err := ph.Wait(time.Hour); err != nil {
		log.Printf("wait or child failed %s\n", err)
	}
}
