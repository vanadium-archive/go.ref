// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"fmt"
	"os"

	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/internal/binarylib"
)

func main() {
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

var cmdDelete = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runDelete),
	Name:     "delete",
	Short:    "Delete a binary",
	Long:     "Delete connects to the binary repository and deletes the specified binary",
	ArgsName: "<von>",
	ArgsLong: "<von> is the vanadium object name of the binary to delete",
}

func runDelete(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("delete: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von := args[0]
	if err := binarylib.Delete(ctx, von); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Binary deleted successfully\n")
	return nil
}

var cmdDownload = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runDownload),
	Name:   "download",
	Short:  "Download a binary",
	Long: `
Download connects to the binary repository, downloads the specified binary, and
writes it to a file.
`,
	ArgsName: "<von> <filename>",
	ArgsLong: `
<von> is the vanadium object name of the binary to download
<filename> is the name of the file where the binary will be written
`,
}

func runDownload(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return env.UsageErrorf("download: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von, filename := args[0], args[1]
	if err := binarylib.DownloadToFile(ctx, von, filename); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Binary downloaded to file %s\n", filename)
	return nil
}

var cmdUpload = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runUpload),
	Name:   "upload",
	Short:  "Upload a binary or directory archive",
	Long: `
Upload connects to the binary repository and uploads the binary of the specified
file or archive of the specified directory. When successful, it writes the name of the new binary to stdout.
`,
	ArgsName: "<von> <filename>",
	ArgsLong: `
<von> is the vanadium object name of the binary to upload
<filename> is the name of the file or directory to upload
`,
}

func runUpload(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return env.UsageErrorf("upload: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von, filename := args[0], args[1]
	fi, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		sig, err := binarylib.UploadFromDir(ctx, von, filename)
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "Binary package uploaded from directory %s signature(%v)\n", filename, sig)
		return nil
	}
	sig, err := binarylib.UploadFromFile(ctx, von, filename)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Binary uploaded from file %s signature(%v)\n", filename, sig)
	return nil
}

var cmdURL = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runURL),
	Name:     "url",
	Short:    "Fetch a download URL",
	Long:     "Connect to the binary repository and fetch the download URL for the given vanadium object name.",
	ArgsName: "<von>",
	ArgsLong: "<von> is the vanadium object name of the binary repository",
}

func runURL(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("rooturl: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von := args[0]
	url, _, err := binarylib.DownloadUrl(ctx, von)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "%v\n", url)
	return nil
}

var cmdRoot = &cmdline.Command{
	Name:  "binary",
	Short: "manages the Vanadium binary repository",
	Long: `
Command binary manages the Vanadium binary repository.
`,
	Children: []*cmdline.Command{cmdDelete, cmdDownload, cmdUpload, cmdURL},
}
