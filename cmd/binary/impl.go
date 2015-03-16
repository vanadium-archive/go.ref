package main

import (
	"fmt"
	"os"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/services/mgmt/lib/binary"
)

var cmdDelete = &cmdline.Command{
	Run:      runDelete,
	Name:     "delete",
	Short:    "Delete a binary",
	Long:     "Delete connects to the binary repository and deletes the specified binary",
	ArgsName: "<von>",
	ArgsLong: "<von> is the veyron object name of the binary to delete",
}

func runDelete(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("delete: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von := args[0]
	if err := binary.Delete(gctx, von); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Binary deleted successfully\n")
	return nil
}

var cmdDownload = &cmdline.Command{
	Run:   runDownload,
	Name:  "download",
	Short: "Download a binary",
	Long: `
Download connects to the binary repository, downloads the specified binary, and
writes it to a file.
`,
	ArgsName: "<von> <filename>",
	ArgsLong: `
<von> is the veyron object name of the binary to download
<filename> is the name of the file where the binary will be written
`,
}

func runDownload(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("download: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von, filename := args[0], args[1]
	if err := binary.DownloadToFile(gctx, von, filename); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Binary downloaded to file %s\n", filename)
	return nil
}

var cmdUpload = &cmdline.Command{
	Run:   runUpload,
	Name:  "upload",
	Short: "Upload a binary or directory archive",
	Long: `
Upload connects to the binary repository and uploads the binary of the specified
file or archive of the specified directory. When successful, it writes the name of the new binary to stdout.
`,
	ArgsName: "<von> <filename>",
	ArgsLong: `
<von> is the veyron object name of the binary to upload
<filename> is the name of the file or directory to upload
`,
}

func runUpload(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("upload: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von, filename := args[0], args[1]
	fi, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		sig, err := binary.UploadFromDir(gctx, von, filename)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.Stdout(), "Binary package uploaded from directory %s signature(%v)\n", filename, sig)
		return nil
	}
	sig, err := binary.UploadFromFile(gctx, von, filename)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Binary uploaded from file %s signature(%v)\n", filename, sig)
	return nil
}

var cmdURL = &cmdline.Command{
	Run:      runURL,
	Name:     "url",
	Short:    "Fetch a download URL",
	Long:     "Connect to the binary repository and fetch the download URL for the given veyron object name.",
	ArgsName: "<von>",
	ArgsLong: "<von> is the veyron object name of the binary repository",
}

func runURL(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("rooturl: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	von := args[0]
	url, _, err := binary.DownloadUrl(gctx, von)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "%v\n", url)
	return nil
}

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "binary",
		Short: "Tool for interacting with the veyron binary repository",
		Long: `
The binary tool facilitates interaction with the veyron binary repository.
`,
		Children: []*cmdline.Command{cmdDelete, cmdDownload, cmdUpload, cmdURL},
	}
}
