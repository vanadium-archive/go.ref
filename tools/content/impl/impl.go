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
	Short:    "Delete content",
	Long:     "Delete connects to the content repository and deletes the specified content",
	ArgsName: "<content>",
	ArgsLong: "<content> is the full name of the content to delete.",
}

func runDelete(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.Errorf("delete: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	c, err := repository.BindContent(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	if err = c.Delete(rt.R().NewContext()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Content deleted successfully\n")
	return nil
}

var cmdDownload = &cmdline.Command{
	Run:   runDownload,
	Name:  "download",
	Short: "Download content",
	Long: `
Download connects to the content repository, downloads the specified content, and
writes it to a file.
`,
	ArgsName: "<content> <filename>",
	ArgsLong: `
<content> is the full name of the content to download
<filename> is the name of the file where the content will be written
`,
}

func runDownload(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("download: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	f, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open %q: %v", args[0], err)
	}
	defer f.Close()

	c, err := repository.BindContent(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}

	stream, err := c.Download(rt.R().NewContext())
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

	fmt.Fprintf(cmd.Stdout(), "Content downloaded to file %s\n", args[1])
	return nil
}

var cmdUpload = &cmdline.Command{
	Run:   runUpload,
	Name:  "upload",
	Short: "Upload content",
	Long: `
Upload connects to the content repository and uploads the content of the specified
file. When successful, it writes the name of the new content to stdout.
`,
	ArgsName: "<server> <filename>",
	ArgsLong: `
<server> is the veyron name or endpoint of the content repository.
<filename> is the name of the file to upload.
`,
}

func runUpload(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.Errorf("upload: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	f, err := os.Open(args[1])
	if err != nil {
		return fmt.Errorf("failed to open %q: %v", args[1], err)
	}
	defer f.Close()

	c, err := repository.BindContent(args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}

	stream, err := c.Upload(rt.R().NewContext())
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
		if err = stream.Send(buf[:n]); err != nil {
			return fmt.Errorf("send error: %v", err)
		}
	}
	if err = stream.CloseSend(); err != nil {
		return fmt.Errorf("closesend error: %v", err)
	}

	name, err := stream.Finish()
	if err != nil {
		return fmt.Errorf("finish error: %v", err)
	}

	fmt.Fprintf(cmd.Stdout(), "%s\n", name)
	return nil
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:     "content",
		Short:    "Command-line tool for interacting with the veyron content repository",
		Long:     "Command-line tool for interacting with the veyron content repository",
		Children: []*cmdline.Command{cmdDelete, cmdDownload, cmdUpload},
	}
}
