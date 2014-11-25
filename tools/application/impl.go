package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	"veyron.io/lib/cmdline"
	"veyron.io/veyron/veyron/services/mgmt/repository"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/services/mgmt/application"
)

func getEnvelopeJSON(ctx context.T, app repository.ApplicationClientMethods, profiles string) ([]byte, error) {
	env, err := app.Match(ctx, strings.Split(profiles, ","))
	if err != nil {
		return nil, err
	}
	j, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("MarshalIndent(%v) failed: %v", env, err)
	}
	return j, nil
}

func putEnvelopeJSON(ctx context.T, app repository.ApplicationClientMethods, profiles string, j []byte) error {
	var env application.Envelope
	if err := json.Unmarshal(j, &env); err != nil {
		return fmt.Errorf("Unmarshal(%v) failed: %v", string(j), err)
	}
	if err := app.Put(ctx, strings.Split(profiles, ","), env); err != nil {
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
		return cmd.UsageErrorf("match: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name, profiles := args[0], args[1]
	app := repository.ApplicationClient(name)
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	j, err := getEnvelopeJSON(ctx, app, profiles)
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
		return cmd.UsageErrorf("put: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name, profiles, envelope := args[0], args[1], args[2]
	app := repository.ApplicationClient(name)
	j, err := ioutil.ReadFile(envelope)
	if err != nil {
		return fmt.Errorf("ReadFile(%v): %v", envelope, err)
	}
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	if err = putEnvelopeJSON(ctx, app, profiles, j); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Application envelope added successfully.")
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
		return cmd.UsageErrorf("remove: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name, profile := args[0], args[1]
	app := repository.ApplicationClient(name)
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	if err := app.Remove(ctx, profile); err != nil {
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
		return cmd.UsageErrorf("edit: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name, profile := args[0], args[1]
	app := repository.ApplicationClient(name)
	f, err := ioutil.TempFile("", "application-edit-")
	if err != nil {
		return fmt.Errorf("TempFile() failed: %v", err)
	}
	fileName := f.Name()
	f.Close()
	defer os.Remove(fileName)

	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	envData, err := getEnvelopeJSON(ctx, app, profile)
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
		if err = putEnvelopeJSON(ctx, app, profile, newData); err != nil {
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

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "application",
		Short: "Tool for interacting with the veyron application repository",
		Long: `
The application tool facilitates interaction with the veyron application
repository.
`,
		Children: []*cmdline.Command{cmdMatch, cmdPut, cmdRemove, cmdEdit},
	}
}
