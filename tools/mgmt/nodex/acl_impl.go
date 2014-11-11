package main

// Commands to get/set ACLs.

import (
	"fmt"
	"sort"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/node"

	"veyron.io/veyron/veyron/lib/cmdline"
)

var cmdGet = &cmdline.Command{
	Run:      runGet,
	Name:     "get",
	Short:    "Get ACLs for the given target.",
	Long:     "Get ACLs for the given target with friendly output. Also see getraw.",
	ArgsName: "<node manager name>",
	ArgsLong: `
<node manager name> can be a Vanadium name for a node manager,
application installation or instance.`,
}

type formattedACLEntry struct {
	blessing string
	inout    string
	label    string
}

// Code to make formattedACLEntry sorted.
type byBlessing []formattedACLEntry

func (a byBlessing) Len() int           { return len(a) }
func (a byBlessing) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byBlessing) Less(i, j int) bool { return a[i].blessing < a[j].blessing }

func runGet(cmd *cmdline.Command, args []string) error {

	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("install: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	vanaName := args[0]
	objACL, _, err := node.ApplicationClient(vanaName).GetACL(rt.R().NewContext())
	if err != nil {
		return fmt.Errorf("GetACL on %s failed: %v", vanaName, err)
	}

	// TODO(rjkroege): Update for custom labels.
	output := make([]formattedACLEntry, 0)
	for k, _ := range objACL.In {
		output = append(output, formattedACLEntry{string(k), "in", objACL.In[k].String()})
	}
	for k, _ := range objACL.NotIn {

		output = append(output, formattedACLEntry{string(k), "nin", objACL.NotIn[k].String()})
	}

	sort.Sort(byBlessing(output))

	for _, e := range output {
		fmt.Fprintf(cmd.Stdout(), "%s %s %s\n", e.blessing, e.inout, e.label)
	}
	return nil
}

// TODO(rjkroege): Implement the remaining sub-commands.
// nodex acl set <target>  ([!]<label> <blessing>)...

func aclRoot() *cmdline.Command {
	return &cmdline.Command{
		Name:  "acl",
		Short: "Tool for creating associations between Vanadium blessings and a system account",
		Long: `
The associate tool facilitates managing blessing to system account associations.
`,
		Children: []*cmdline.Command{cmdGet},
	}
}
