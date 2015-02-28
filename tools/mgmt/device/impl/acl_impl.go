package impl

// Commands to get/set ACLs.

import (
	"fmt"

	"v.io/v23/security"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
)

var cmdGet = &cmdline.Command{
	Run:      runGet,
	Name:     "get",
	Short:    "Get ACLs for the given target.",
	Long:     "Get ACLs for the given target.",
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
	objACL, _, err := device.ApplicationClient(vanaName).GetACL(gctx)
	if err != nil {
		return fmt.Errorf("GetACL on %s failed: %v", vanaName, err)
	}
	// Convert objACL (TaggedACLMap) into aclEntries for pretty printing.
	entries := make(aclEntries)
	for tag, acl := range objACL {
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

var cmdSet = &cmdline.Command{
	Run:      runSet,
	Name:     "set",
	Short:    "Set ACLs for the given target.",
	Long:     "Set ACLs for the given target",
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
<blessing> in the current ACL to be cleared.

Examples:
set root/self ^
will remove "root/self" from the In and NotIn lists for all access rights.

set root/self Read,!Write
will add "root/self" to the In list for Read access and the NotIn list
for Write access (and remove "root/self" from both the In and NotIn
lists of all other access rights)`,
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

	// Set the ACLs on the specified names.
	for {
		objACL, etag, err := device.ApplicationClient(vanaName).GetACL(gctx)
		if err != nil {
			return cmd.UsageErrorf("GetACL(%s) failed: %v", vanaName, err)
		}
		for blessingOrPattern, tags := range entries {
			objACL.Clear(blessingOrPattern) // Clear out any existing references
			for tag, blacklist := range tags {
				if blacklist {
					objACL.Blacklist(blessingOrPattern, tag)
				} else {
					objACL.Add(security.BlessingPattern(blessingOrPattern), tag)
				}
			}
		}
		switch err := device.ApplicationClient(vanaName).SetACL(gctx, objACL, etag); {
		case err != nil && !verror.Is(err, verror.ErrBadEtag.ID):
			return cmd.UsageErrorf("SetACL(%s) failed: %v", vanaName, err)
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
		Short: "Tool for setting device manager ACLs",
		Long: `
The acl tool manages ACLs on the device manger, installations and instances.
`,
		Children: []*cmdline.Command{cmdGet, cmdSet},
	}
}
