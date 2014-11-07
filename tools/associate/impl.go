package main

import (
	"fmt"
	"time"

	"veyron.io/veyron/veyron/lib/cmdline"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mgmt/node"
)

var cmdList = &cmdline.Command{
	Run:      runList,
	Name:     "list",
	Short:    "Lists the account associations.",
	Long:     "Lists all account associations.",
	ArgsName: "<nodemanager>.",
	ArgsLong: `
<nodemanager> is the name of the node manager to connect to.`,
}

func runList(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("list: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	assocs, err := node.NodeClient(args[0]).ListAssociations(ctx)
	if err != nil {
		return fmt.Errorf("ListAssociations failed: %v", err)
	}

	for _, a := range assocs {
		fmt.Fprintf(cmd.Stdout(), "%s %s\n", a.IdentityName, a.AccountName)
	}
	return nil
}

var cmdAdd = &cmdline.Command{
	Run:      runAdd,
	Name:     "add",
	Short:    "Add the listed blessings with the specified system account.",
	Long:     "Add the listed blessings with the specified system account.",
	ArgsName: "<nodemanager> <systemName> <blessing>...",
	ArgsLong: `
<nodemanager> is the name of the node manager to connect to.
<systemName> is the name of an account holder on the local system.
<blessing>.. are the blessings to associate systemAccount with.`,
}

func runAdd(cmd *cmdline.Command, args []string) error {
	if expected, got := 3, len(args); got < expected {
		return cmd.UsageErrorf("add: incorrect number of arguments, expected at least %d, got %d", expected, got)
	}
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	return node.NodeClient(args[0]).AssociateAccount(ctx, args[2:], args[1])
}

var cmdRemove = &cmdline.Command{
	Run:      runRemove,
	Name:     "remove",
	Short:    "Removes system accounts associated with the listed blessings.",
	Long:     "Removes system accounts associated with the listed blessings.",
	ArgsName: "<nodemanager>  <blessing>...",
	ArgsLong: `
<nodemanager> is the name of the node manager to connect to.
<blessing>... is a list of blessings.`,
}

func runRemove(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); got < expected {
		return cmd.UsageErrorf("remove: incorrect number of arguments, expected at least %d, got %d", expected, got)
	}
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	return node.NodeClient(args[0]).AssociateAccount(ctx, args[1:], "")
}

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "associate",
		Short: "Tool for creating associations between Vanadium blessings and a system account",
		Long: `
The associate tool facilitates managing blessing to system account associations.
`,
		Children: []*cmdline.Command{cmdList, cmdAdd, cmdRemove},
	}
}
