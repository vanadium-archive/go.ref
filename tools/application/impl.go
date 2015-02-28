package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"v.io/core/veyron/services/mgmt/repository"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/vom"
	"v.io/x/lib/cmdline"
)

// TODO(ashankar): application.Envelope is no longer JSON friendly
// (after https://vanadium-review.googlesource.com/#/c/6300/).
//
// This mirrored structure is required in order to work around that
// problem. Figure out what we want to do.
type appenv struct {
	Title     string
	Args      []string
	Binary    application.SignedFile
	Publisher security.WireBlessings
	Env       []string
	Packages  application.Packages
}

func (a *appenv) Load(env application.Envelope) {
	a.Title = env.Title
	a.Args = env.Args
	a.Binary = env.Binary
	a.Publisher = security.MarshalBlessings(env.Publisher)
	a.Env = env.Env
	a.Packages = env.Packages
}

func (a *appenv) Store() (application.Envelope, error) {
	// Have to roundtrip through vom to convert from WireBlessings to Blessings.
	// This may seem silly, but this whole appenv type is silly too :).
	// Figure out how to get rid of it.
	bytes, err := vom.Encode(a.Publisher)
	if err != nil {
		return application.Envelope{}, err
	}
	var publisher security.Blessings
	if err := vom.Decode(bytes, &publisher); err != nil {
		return application.Envelope{}, err
	}
	return application.Envelope{
		Title:     a.Title,
		Args:      a.Args,
		Binary:    a.Binary,
		Publisher: publisher,
		Env:       a.Env,
		Packages:  a.Packages,
	}, nil
}

func init() {
	// Ensure that no fields have been added to application.Envelope,
	// because if so, then appenv needs to change.
	if n := reflect.TypeOf(application.Envelope{}).NumField(); n != 6 {
		panic(fmt.Sprintf("It appears that fields have been added to or removed from application.Envelope before the hack in this file around json-encodeability was removed. Please also update appenv, appenv.Load and appenv.Store in this file"))
	}
}

func getEnvelopeJSON(app repository.ApplicationClientMethods, profiles string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()
	env, err := app.Match(ctx, strings.Split(profiles, ","))
	if err != nil {
		return nil, err
	}
	var appenv appenv
	appenv.Load(env)
	j, err := json.MarshalIndent(appenv, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("MarshalIndent(%v) failed: %v", env, err)
	}
	return j, nil
}

func putEnvelopeJSON(app repository.ApplicationClientMethods, profiles string, j []byte) error {
	var appenv appenv
	if err := json.Unmarshal(j, &appenv); err != nil {
		return fmt.Errorf("Unmarshal(%v) failed: %v", string(j), err)
	}
	env, err := appenv.Store()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()
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
	j, err := getEnvelopeJSON(app, profiles)
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
	ArgsName: "<application> <profiles> [<envelope>]",
	ArgsLong: `
<application> is the full name of the application.
<profiles> is a comma-separated list of profiles.
<envelope> is the file that contains a JSON-encoded envelope. If this file is
not provided, the user will be prompted to enter the data manually.`,
}

func runPut(cmd *cmdline.Command, args []string) error {
	if got := len(args); got != 2 && got != 3 {
		return cmd.UsageErrorf("put: incorrect number of arguments, expected 2 or 3, got %d", got)
	}
	name, profiles := args[0], args[1]
	app := repository.ApplicationClient(name)
	if len(args) == 3 {
		envelope := args[2]
		j, err := ioutil.ReadFile(envelope)
		if err != nil {
			return fmt.Errorf("ReadFile(%v): %v", envelope, err)
		}
		if err = putEnvelopeJSON(app, profiles, j); err != nil {
			return err
		}
		fmt.Fprintln(cmd.Stdout(), "Application envelope added successfully.")
		return nil
	}
	env := application.Envelope{Args: []string{}, Env: []string{}, Packages: application.Packages{}}
	j, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("MarshalIndent() failed: %v", err)
	}
	if err := editAndPutEnvelopeJSON(cmd, app, profiles, j); err != nil {
		return err
	}
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
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
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

	envData, err := getEnvelopeJSON(app, profile)
	if err != nil {
		return err
	}
	if err := editAndPutEnvelopeJSON(cmd, app, profile, envData); err != nil {
		return err
	}
	return nil
}

func editAndPutEnvelopeJSON(cmd *cmdline.Command, app repository.ApplicationClientMethods, profile string, envData []byte) error {
	f, err := ioutil.TempFile("", "application-edit-")
	if err != nil {
		return fmt.Errorf("TempFile() failed: %v", err)
	}
	fileName := f.Name()
	f.Close()
	defer os.Remove(fileName)
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
		if err = putEnvelopeJSON(app, profile, newData); err != nil {
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
