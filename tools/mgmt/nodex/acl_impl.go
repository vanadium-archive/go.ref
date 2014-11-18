package main

// Commands to get/set ACLs.

import (
	"fmt"
	"sort"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/services/security/access"
	"veyron.io/veyron/veyron2/verror"

	"veyron.io/veyron/veyron/lib/cmdline"
)

var cmdGet = &cmdline.Command{
	Run:      runGet,
	Name:     "get",
	Short:    "Get ACLs for the given target.",
	Long:     "Get ACLs for the given target.",
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
		return cmd.UsageErrorf("get: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	vanaName := args[0]
	objACL, _, err := node.ApplicationClient(vanaName).GetACL(rt.R().NewContext())
	if err != nil {
		return fmt.Errorf("GetACL on %s failed: %v", vanaName, err)
	}

	// TODO(rjkroege): Update for custom labels.
	output := make([]formattedACLEntry, 0)
	for k, _ := range objACL.In {
		output = append(output, formattedACLEntry{string(k), "", objACL.In[k].String()})
	}
	for k, _ := range objACL.NotIn {
		output = append(output, formattedACLEntry{string(k), "!", objACL.NotIn[k].String()})
	}

	sort.Sort(byBlessing(output))

	for _, e := range output {
		fmt.Fprintf(cmd.Stdout(), "%s %s%s\n", e.blessing, e.inout, e.label)
	}
	return nil
}

var cmdSet = &cmdline.Command{
	Run:      runSet,
	Name:     "set",
	Short:    "Set ACLs for the given target.",
	Long:     "Set ACLs for the given target",
	ArgsName: "<node manager name>  (<blessing> [!]<label>)...",
	ArgsLong: `
<node manager name> can be a Vanadium name for a node manager,
application installation or instance.

<blessing> is a blessing pattern.

<label> is a character sequence defining a set of rights: some subset
of the defined standard Vanadium labels of XRWADM where X is resolve,
R is read, W for write, A for admin, D for debug and M is for
monitoring. By default, the combination of <blessing>, <label>
replaces whatever entry is present in the ACL's In field for the
<blessing> but it can instead be added to the NotIn field by prefacing
<label> with a '!' character. Use the <label> of 0 to clear the label.

For example: root/self !0 will clear the NotIn field for blessingroot/self.`,
}

type inAdditionTuple struct {
	blessing security.BlessingPattern
	ls       *security.LabelSet
}

type notInAdditionTuple struct {
	blessing string
	ls       *security.LabelSet
}

func runSet(cmd *cmdline.Command, args []string) error {
	if got := len(args); !((got%2) == 1 && got >= 3) {
		return cmd.UsageErrorf("set: incorrect number of arguments %d, must be 1 + 2n", got)
	}

	vanaName := args[0]
	pairs := args[1:]

	// Parse each pair and aggregate what should happen to all of them
	notInDeletions := make([]string, 0)
	inDeletions := make([]security.BlessingPattern, 0)
	inAdditions := make([]inAdditionTuple, 0)
	notInAdditions := make([]notInAdditionTuple, 0)

	for i := 0; i < len(pairs); i += 2 {
		blessing, label := pairs[i], pairs[i+1]
		if label == "" || label == "!" {
			return cmd.UsageErrorf("failed to parse LabelSet pair %s, %s", blessing, label)
		}

		switch {
		case label == "!0":
			notInDeletions = append(notInDeletions, blessing)
		case label == "0":
			inDeletions = append(inDeletions, security.BlessingPattern(blessing))
		case label[0] == '!':
			// e.g. !RW
			ls := new(security.LabelSet)
			if err := ls.FromString(label[1:]); err != nil {
				return cmd.UsageErrorf("failed to parse LabelSet %s:  %v", label, err)
			}
			notInAdditions = append(notInAdditions, notInAdditionTuple{blessing, ls})
		default:
			// e.g. X
			ls := new(security.LabelSet)
			if err := ls.FromString(label); err != nil {
				return fmt.Errorf("failed to parse LabelSet %s:  %v", label, err)
			}
			inAdditions = append(inAdditions, inAdditionTuple{security.BlessingPattern(blessing), ls})
		}
	}

	// Set the ACLs on the specified name.
	for {
		objACL, etag, err := node.ApplicationClient(vanaName).GetACL(rt.R().NewContext())
		if err != nil {
			return cmd.UsageErrorf("GetACL(%s) failed: %v", vanaName, err)
		}

		// Insert into objACL
		for _, b := range notInDeletions {
			if _, contains := objACL.NotIn[b]; !contains {
				fmt.Fprintf(cmd.Stderr(), "WARNING: ignoring attempt to remove non-existing NotIn ACL for %s\n", b)
			}
			delete(objACL.NotIn, b)
		}

		for _, b := range inDeletions {
			if _, contains := objACL.In[b]; !contains {
				fmt.Fprintf(cmd.Stderr(), "WARNING: ignoring attempt to remove non-existing In ACL for %s\n", b)
			}
			delete(objACL.In, b)
		}

		for _, b := range inAdditions {
			objACL.In[b.blessing] = *b.ls
		}

		for _, b := range notInAdditions {
			objACL.NotIn[b.blessing] = *b.ls
		}

		switch err := node.ApplicationClient(vanaName).SetACL(rt.R().NewContext(), objACL, etag); {
		case err != nil && !verror.Is(err, access.ErrBadEtag):
			return cmd.UsageErrorf("SetACL(%s) failed: %v", vanaName, err)
		case err == nil:
			return nil
		}
		fmt.Fprintf(cmd.Stderr(), "WARNING: trying again because of asynchronous change\n")
	}
	return nil
}

func aclRoot() *cmdline.Command {
	return &cmdline.Command{
		Name:  "acl",
		Short: "Tool for setting node manager ACLs",
		Long: `
The acl tool manages ACLs on the node manger, installations and instances.
`,
		Children: []*cmdline.Command{cmdGet, cmdSet},
	}
}
