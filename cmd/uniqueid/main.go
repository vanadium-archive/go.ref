// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $VANADIUM_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"

	"v.io/v23"
	"v.io/v23/uniqueid"
	"v.io/x/lib/cmdline"
	_ "v.io/x/ref/profiles/static"
)

func main() {
	os.Exit(cmdUniqueId.Main())
}

func runHelper(run cmdline.Runner) cmdline.Runner {
	return func(cmd *cmdline.Command, args []string) error {
		_, shutdown := v23.Init()
		defer shutdown()
		return run(cmd, args)
	}
}

var cmdUniqueId = &cmdline.Command{
	Name:  "uniqueid",
	Short: "Generates UniqueIds.",
	Long: `
The uniqueid tool generates unique ids.
It also has an option of automatically substituting unique ids with placeholders in files.
`,
	Children: []*cmdline.Command{cmdGenerate, cmdInject},
	Topics:   []cmdline.Topic{},
}

var cmdGenerate = &cmdline.Command{
	Run:   runHelper(runGenerate),
	Name:  "generate",
	Short: "Generates UniqueIds",
	Long: `
Generates unique ids and outputs them to standard out.
`,
	ArgsName: "",
	ArgsLong: "",
}

var cmdInject = &cmdline.Command{
	Run:   runHelper(runInject),
	Name:  "inject",
	Short: "Injects UniqueIds into existing files",
	Long: `
Injects UniqueIds into existing files.
Strings of the form "$UNIQUEID$" will be replaced with generated ids.
`,
	ArgsName: "<filenames>",
	ArgsLong: "<filenames> List of files to inject unique ids into",
}

// runGenerate implements the generate command which outputs generated ids to stdout.
func runGenerate(command *cmdline.Command, args []string) error {
	if len(args) > 0 {
		return command.UsageErrorf("expected 0 args, got %d", len(args))
	}
	id, err := uniqueid.Random()
	if err != nil {
		return err
	}
	fmt.Printf("%#v\n", id)
	return nil
}

// runInject implements the inject command which replaces $UNIQUEID$ strings with generated ids.
func runInject(command *cmdline.Command, args []string) error {
	if len(args) == 0 {
		return command.UsageErrorf("expected at least one file arg, got 0")
	}
	for _, arg := range args {
		if err := injectIntoFile(arg); err != nil {
			return err
		}
	}
	return nil
}

// injectIntoFile replaces $UNIQUEID$ strings when they exist in the specified file.
func injectIntoFile(filename string) error {
	inbytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	// Replace $UNIQUEID$ with generated ids.
	re, err := regexp.Compile("[$]UNIQUEID")
	if err != nil {
		return err
	}
	replaced := re.ReplaceAllFunc(inbytes, func(match []byte) []byte {
		id, randErr := uniqueid.Random()
		if randErr != nil {
			err = randErr
		}
		return []byte(fmt.Sprintf("%#v", id))
	})
	if err != nil {
		return err
	}

	// If the file with injections is different, write it to disk.
	if !bytes.Equal(inbytes, replaced) {
		fmt.Printf("Updated: %s\n", filename)
		return ioutil.WriteFile(filename, replaced, 0)
	}
	return nil
}
