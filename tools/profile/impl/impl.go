package impl

import (
	"fmt"

	"veyron/lib/cmdline"
	"veyron/services/mgmt/profile"
	"veyron/services/mgmt/repository"

	"veyron2/rt"
)

var cmdLabel = &cmdline.Command{
	Run:      runLabel,
	Name:     "label",
	Short:    "Shows a human-readable profile key for the profile.",
	Long:     "Shows a human-readable profile key for the profile.",
	ArgsName: "<profile>",
	ArgsLong: "<profile> is the full name of the profile.",
}

func runLabel(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("label: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := repository.BindProfile(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	label, err := p.Label(rt.R().NewContext())
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), label)
	return nil
}

var cmdDescription = &cmdline.Command{
	Run:      runDescription,
	Name:     "description",
	Short:    "Shows a human-readable profile description for the profile.",
	Long:     "Shows a human-readable profile description for the profile.",
	ArgsName: "<profile>",
	ArgsLong: "<profile> is the full name of the profile.",
}

func runDescription(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("description: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := repository.BindProfile(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	desc, err := p.Description(rt.R().NewContext())
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), desc)
	return nil
}

var cmdSpec = &cmdline.Command{
	Run:      runSpec,
	Name:     "spec",
	Short:    "Shows the specification of the profile.",
	Long:     "Shows the specification of the profile.",
	ArgsName: "<profile>",
	ArgsLong: "<profile> is the full name of the profile.",
}

func runSpec(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("spec: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := repository.BindProfile(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	spec, err := p.Specification(rt.R().NewContext())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "%#v\n", spec)
	return nil
}

var cmdPut = &cmdline.Command{
	Run:      runPut,
	Name:     "put",
	Short:    "Sets a placeholder specification for the profile.",
	Long:     "Sets a placeholder specification for the profile.",
	ArgsName: "<profile>",
	ArgsLong: "<profile> is the full name of the profile.",
}

func runPut(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("put: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := repository.BindProfile(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}

	// TODO(rthellend): Read an actual specification from a file.
	spec := profile.Specification{
		Format:      profile.Format{Name: "elf", Attributes: map[string]string{"os": "linux", "arch": "amd64"}},
		Libraries:   map[profile.Library]struct{}{profile.Library{Name: "foo", MajorVersion: "1", MinorVersion: "0"}: struct{}{}},
		Label:       "example",
		Description: "Example profile to test the profile manager implementation.",
	}
	if err := p.Put(rt.R().NewContext(), spec); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Specification updated successfully.")
	return nil
}

var cmdRemove = &cmdline.Command{
	Run:      runRemove,
	Name:     "remove",
	Short:    "removes the profile specification for the profile.",
	Long:     "removes the profile specification for the profile.",
	ArgsName: "<profile>",
	ArgsLong: "<profile> is the full name of the profile.",
}

func runRemove(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("remove: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	p, err := repository.BindProfile(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	if err = p.Remove(rt.R().NewContext()); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Profile removed successfully.")
	return nil
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:     "profile",
		Short:    "Command-line tool for interacting with the veyron profile repository",
		Long:     "Command-line tool for interacting with the veyron profile repository",
		Children: []*cmdline.Command{cmdLabel, cmdDescription, cmdSpec, cmdPut, cmdRemove},
	}
}
