// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// Commands to get/set AccessLists.

import (
	"fmt"

	"v.io/v23/security"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
)

var cmdGet = &cmdline.Command{
	Run:      runGet,
	Name:     "get",
	Short:    "Get AccessLists for the given target.",
	Long:     "Get AccessLists for the given target.",
	ArgsName: "<device manager name>",
	ArgsLong: `
<device manager name> can be a Vanadium name for a device manager,
application installation or instance.`,
}

func runGet(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("get: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	vanaName := args[0]
	objAccessList, _, err := device.ApplicationClient(vanaName).GetPermissions(gctx)
	if err != nil {
		return fmt.Errorf("GetPermissions on %s failed: %v", vanaName, err)
	}
	// Convert objAccessList (Permissions) into aclEntries for pretty printing.
	entries := make(aclEntries)
	for tag, acl := range objAccessList {
		for _, p := range acl.In {
			entries.Tags(string(p))[tag] = false
		}
		for _, b := range acl.NotIn {
			entries.Tags(b)[tag] = true
		}
	}
	fmt.Fprintln(cmd.Stdout(), entries)
	return nil
}

// TODO(caprita): Add unit test logic for 'force set'.
var forceSet bool

var cmdSet = &cmdline.Command{
	Run:      runSet,
	Name:     "set",
	Short:    "Set AccessLists for the given target.",
	Long:     "Set AccessLists for the given target",
	ArgsName: "<device manager name>  (<blessing> [!]<tag>(,[!]<tag>)*",
	ArgsLong: `
<device manager name> can be a Vanadium name for a device manager,
application installation or instance.

<blessing> is a blessing pattern.
If the same pattern is repeated multiple times in the command, then
the only the last occurrence will be honored.

<tag> is a subset of defined access types ("Admin", "Read", "Write" etc.).
If the access right is prefixed with a '!' then <blessing> is added to the
NotIn list for that right. Using "^" as a "tag" causes all occurrences of
<blessing> in the current AccessList to be cleared.

Examples:
set root/self ^
will remove "root/self" from the In and NotIn lists for all access rights.

set root/self Read,!Write
will add "root/self" to the In list for Read access and the NotIn list
for Write access (and remove "root/self" from both the In and NotIn
lists of all other access rights)`,
}

func init() {
	cmdSet.Flags.BoolVar(&forceSet, "f", false, "Instead of making the AccessLists additive, do a complete replacement based on the specified settings.")
}

func runSet(cmd *cmdline.Command, args []string) error {
	if got := len(args); !((got%2) == 1 && got >= 3) {
		return cmd.UsageErrorf("set: incorrect number of arguments %d, must be 1 + 2n", got)
	}

	vanaName := args[0]
	pairs := args[1:]

	entries := make(aclEntries)
	for i := 0; i < len(pairs); i += 2 {
		blessing := pairs[i]
		tags, err := parseAccessTags(pairs[i+1])
		if err != nil {
			return cmd.UsageErrorf("failed to parse access tags for %q: %v", blessing, err)
		}
		entries[blessing] = tags
	}

	// Set the AccessLists on the specified names.
	for {
		objAccessList, etag := make(access.Permissions), ""
		if !forceSet {
			var err error
			if objAccessList, etag, err = device.ApplicationClient(vanaName).GetPermissions(gctx); err != nil {
				return fmt.Errorf("GetPermissions(%s) failed: %v", vanaName, err)
			}
		}
		for blessingOrPattern, tags := range entries {
			objAccessList.Clear(blessingOrPattern) // Clear out any existing references
			for tag, blacklist := range tags {
				if blacklist {
					objAccessList.Blacklist(blessingOrPattern, tag)
				} else {
					objAccessList.Add(security.BlessingPattern(blessingOrPattern), tag)
				}
			}
		}
		switch err := device.ApplicationClient(vanaName).SetPermissions(gctx, objAccessList, etag); {
		case err != nil && verror.ErrorID(err) != verror.ErrBadEtag.ID:
			return fmt.Errorf("SetPermissions(%s) failed: %v", vanaName, err)
		case err == nil:
			return nil
		}
		fmt.Fprintln(cmd.Stderr(), "WARNING: trying again because of asynchronous change")
	}
	return nil
}

func aclRoot() *cmdline.Command {
	return &cmdline.Command{
		Name:  "acl",
		Short: "Tool for setting device manager AccessLists",
		Long: `
The acl tool manages AccessLists on the device manger, installations and instances.
`,
		Children: []*cmdline.Command{cmdGet, cmdSet},
	}
}
