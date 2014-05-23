package impl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"veyron/lib/cmdline"
	iapp "veyron/services/mgmt/application"

	"veyron2/services/mgmt/application"
)

func getEnvelopeJSON(app iapp.Repository, profiles string) ([]byte, error) {
	env, err := app.Match(strings.Split(profiles, ","))
	if err != nil {
		env = application.Envelope{}
	}
	j, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("json: %v", err)
	}
	return j, nil
}

func putEnvelopeJSON(app iapp.Repository, profiles string, j []byte) error {
	var env application.Envelope
	if err := json.Unmarshal(j, &env); err != nil {
		return fmt.Errorf("json: %v", err)
	}
	if err := app.Put(strings.Split(profiles, ","), env); err != nil {
		return err
	}
	return nil
}

func promptUser(cmd *cmdline.Command, msg string) string {
	fmt.Fprint(cmd.Stdout(), msg)
	var answer string
	if _, err := fmt.Scanf("%s", &answer); err != nil {
		return ""
	}
	return answer
}

var cmdMatch = &cmdline.Command{
	Run:      runMatch,
	Name:     "match",
	Short:    "Shows the first matching envelope that matches the given profiles.",
	Long:     "Shows the first matching envelope that matches the given profiles.",
	ArgsName: "<application> <profiles>",
	ArgsLong: `
<application> is the full name of the application.
<profiles> is a comma-separated list of profiles.`,
}

func runMatch(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("match: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	app, err := iapp.BindRepository(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	j, err := getEnvelopeJSON(app, args[1])
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), string(j))
	return nil
}

var cmdPut = &cmdline.Command{
	Run:      runPut,
	Name:     "put",
	Short:    "Add the given envelope to the application for the given profiles.",
	Long:     "Add the given envelope to the application for the given profiles.",
	ArgsName: "<application> <profiles> <envelope>",
	ArgsLong: `
<application> is the full name of the application.
<profiles> is a comma-separated list of profiles.
<envelope> is the file that contains a JSON-encoded envelope.`,
}

func runPut(cmd *cmdline.Command, args []string) error {
	if expected, got := 3, len(args); expected != got {
		return cmd.Errorf("put: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	app, err := iapp.BindRepository(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	j, err := ioutil.ReadFile(args[2])
	if err != nil {
		return fmt.Errorf("read file %s: %v", args[2], err)
	}
	if err = putEnvelopeJSON(app, args[1], j); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Application updated successfully.")
	return nil
}

var cmdRemove = &cmdline.Command{
	Run:      runRemove,
	Name:     "remove",
	Short:    "removes the application envelope for the given profile.",
	Long:     "removes the application envelope for the given profile.",
	ArgsName: "<application> <profile>",
	ArgsLong: `
<application> is the full name of the application.
<profile> is a profile.`,
}

func runRemove(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("remove: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	app, err := iapp.BindRepository(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	if err = app.Remove(args[1]); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Application envelope removed successfully.")
	return nil
}

var cmdEdit = &cmdline.Command{
	Run:      runEdit,
	Name:     "edit",
	Short:    "edits the application envelope for the given profile.",
	Long:     "edits the application envelope for the given profile.",
	ArgsName: "<application> <profile>",
	ArgsLong: `
<application> is the full name of the application.
<profile> is a profile.`,
}

func runEdit(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("edit: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	app, err := iapp.BindRepository(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	f, err := ioutil.TempFile("", "application-edit-")
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	fileName := f.Name()
	f.Close()
	defer os.Remove(fileName)

	envData, err := getEnvelopeJSON(app, args[1])
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(fileName, envData, os.FileMode(0644)); err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if len(editor) == 0 {
		editor = "nano"
	}
	for {
		c := exec.Command("sh", "-c", fmt.Sprintf("%s %s", editor, fileName))
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to run %s %s", editor, fileName)
		}
		newData, err := ioutil.ReadFile(fileName)
		if err != nil {
			fmt.Fprintf(cmd.Stdout(), "Error: %v\n", err)
			if ans := promptUser(cmd, "Try again? [y/N] "); strings.ToUpper(ans) == "Y" {
				continue
			}
			return errors.New("aborted")
		}
		if bytes.Compare(envData, newData) == 0 {
			fmt.Fprintln(cmd.Stdout(), "Nothing changed")
			return nil
		}
		if err = putEnvelopeJSON(app, args[1], newData); err != nil {
			fmt.Fprintf(cmd.Stdout(), "Error: %v\n", err)
			if ans := promptUser(cmd, "Try again? [y/N] "); strings.ToUpper(ans) == "Y" {
				continue
			}
			return errors.New("aborted")
		}
		break
	}
	fmt.Fprintln(cmd.Stdout(), "Application envelope updated successfully.")
	return nil
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:     "application",
		Short:    "Command-line tool for interacting with the Veyron application manager",
		Long:     "Command-line tool for interacting with the Veyron application manager",
		Children: []*cmdline.Command{cmdMatch, cmdPut, cmdRemove, cmdEdit},
	}
}
