package impl

import (
	"fmt"
	"io"
	"os"

	"veyron/lib/cmdline"

	"veyron2/rt"
	"veyron2/services/mgmt/repository"
)

var cmdDelete = &cmdline.Command{
	Run:      runDelete,
	Name:     "delete",
	Short:    "Delete binary",
	Long:     "Delete connects to the binary repository and deletes the specified binary",
	ArgsName: "<binary>",
	ArgsLong: "<binary> is the veyron name of the binary to delete",
}

func runDelete(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("delete: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	binary := args[0]

	c, err := repository.BindBinary(binary)
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	if err = c.Delete(rt.R().NewContext()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Binary deleted successfully\n")
	return nil
}

var cmdDownload = &cmdline.Command{
	Run:   runDownload,
	Name:  "download",
	Short: "Download binary",
	Long: `
Download connects to the binary repository, downloads the specified binary, and
writes it to a file.
`,
	ArgsName: "<binary> <filename>",
	ArgsLong: `
<binary> is the veyron name of the binary to download
<filename> is the name of the file where the binary will be written
`,
}

func runDownload(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("download: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	binary, filename := args[0], args[1]
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0700)
	if err != nil {
		return fmt.Errorf("failed to open %q: %v", filename, err)
	}
	defer f.Close()

	c, err := repository.BindBinary(binary)
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}

	// TODO(jsimsa): Replace the code below with a call to a client-side
	// binary library once this library exists. In particular, we should
	// take care of resumption and consistency checking.
	stream, err := c.Download(rt.R().NewContext(), 0)
	if err != nil {
		return err
	}

	for {
		buf, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("recv error: %v", err)
		}
		if _, err = f.Write(buf); err != nil {
			return fmt.Errorf("write error: %v", err)
		}
	}

	err = stream.Finish()
	if err != nil {
		return fmt.Errorf("finish error: %v", err)
	}

	fmt.Fprintf(cmd.Stdout(), "Binary downloaded to file %s\n", filename)
	return nil
}

var cmdUpload = &cmdline.Command{
	Run:   runUpload,
	Name:  "upload",
	Short: "Upload binary",
	Long: `
Upload connects to the binary repository and uploads the binary of the specified
file. When successful, it writes the name of the new binary to stdout.
`,
	ArgsName: "<binary> <filename>",
	ArgsLong: `
<binary> is the veyron name of the binary to upload
<filename> is the name of the file to upload
`,
}

func runUpload(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("upload: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	binary, filename := args[0], args[1]
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open %q: %v", filename, err)
	}
	defer f.Close()

	c, err := repository.BindBinary(binary)
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}

	// TODO(jsimsa): Add support for uploading multi-part binaries.
	if err := c.Create(rt.R().NewContext(), 1); err != nil {
		return err
	}

	stream, err := c.Upload(rt.R().NewContext(), 0)
	if err != nil {
		return err
	}

	var buf [4096]byte
	for {
		n, err := f.Read(buf[:])
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read error: %v", err)
		}
		if err := stream.Send(buf[:n]); err != nil {
			return fmt.Errorf("send error: %v", err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("closesend error: %v", err)
	}

	if err := stream.Finish(); err != nil {
		return fmt.Errorf("finish error: %v", err)
	}

	return nil
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:     "binary",
		Short:    "Command-line tool for interacting with the veyron binary repository",
		Long:     "Command-line tool for interacting with the veyron binary repository",
		Children: []*cmdline.Command{cmdDelete, cmdDownload, cmdUpload},
	}
}
