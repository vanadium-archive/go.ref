// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package device_test

import (
	"fmt"
	"os/user"
	"strconv"
	"strings"

	"v.io/x/ref/test/v23tests"
)

const runTestOnThisPlatform = true
const psFlags = "-ej"

type uidMap map[int]struct{}

func (uids uidMap) findAvailable() (int, error) {
	// Accounts starting at 501 are available. Don't use the largest
	// UID because on a corporate imaged Mac, this will overlap with
	// another employee's UID. Instead, use the first available UID >= 501.
	for newuid := 501; newuid < 1e6; newuid++ {
		if _, ok := uids[newuid]; !ok {
			uids[newuid] = struct{}{}
			return newuid, nil
		}
	}
	return 0, fmt.Errorf("Couldn't find an available UID")
}

func newUidMap(i *v23tests.T) uidMap {
	dsclCmd := i.BinaryFromPath("dscl")

	// `dscl . -list /Users UniqueID` into a datastructure.
	userstring := dsclCmd.Run(".", "-list", "/Users", "UniqueID")
	users := strings.Split(userstring, "\n")

	uids := make(map[int]struct{}, len(users))
	for _, line := range users {
		fields := re.Split(line, -1)
		if len(fields) > 1 {
			if uid, err := strconv.Atoi(fields[1]); err == nil {
				uids[uid] = struct{}{}
			}
		}
	}
	return uids
}

func makeAccount(i *v23tests.T, uid int, uname, fullname string) {
	sudoCmd := i.BinaryFromPath("/usr/bin/sudo")
	dsclCmd := sudoCmd.WithPrefixArgs("dscl", ".", "-create", "/Users/"+uname)

	dsclCmd.Run()
	dsclCmd.Run("UserShell", "/bin/bash")
	dsclCmd.Run("RealName", fullname)
	dsclCmd.Run("UniqueID", strconv.FormatInt(int64(uid), 10))
	dsclCmd.Run("PrimaryGroupID", "20")

}

func makeTestAccounts(i *v23tests.T) {
	_, needVanaErr := user.Lookup("vana")
	_, needDevErr := user.Lookup("devicemanager")

	if needVanaErr == nil && needDevErr == nil {
		return
	}

	uids := newUidMap(i)
	if needVanaErr != nil {
		vanauid, err := uids.findAvailable()
		if err != nil {
			i.Fatalf("Can't make test accounts: %v", err)
		}
		makeAccount(i, vanauid, "vana", "Vanadium White")
	}
	if needDevErr != nil {
		devmgruid, err := uids.findAvailable()
		if err != nil {
			i.Fatalf("Can't make test accounts: %v", err)
		}
		makeAccount(i, devmgruid, "devicemanager", "Devicemanager")
	}
}
