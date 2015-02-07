package impl

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/binary"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/core/veyron2/services/mgmt/repository"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/uniqueid"

	"v.io/core/veyron/services/mgmt/lib/packages"
	"v.io/lib/cmdline"
)

var cmdInstallLocal = &cmdline.Command{
	Run:      runInstallLocal,
	Name:     "install-local",
	Short:    "Install the given application from the local system.",
	Long:     "Install the given application, specified using a local path.",
	ArgsName: "<device> <title> [ENV=VAL ...] binary [--flag=val ...] [PACKAGES path ...]",
	ArgsLong: `
<device> is the veyron object name of the device manager's app service.

<title> is the app title.

This is followed by an arbitrary number of environment variable settings, the
local path for the binary to install, and arbitrary flag settings and args.
Optionally, this can be followed by 'PACKAGES' and a list of local files and
directories to be installed as packages for the app`}

func init() {
	cmdInstallLocal.Flags.Var(&configOverride, "config", "JSON-encoded device.Config object, of the form: '{\"flag1\":\"value1\",\"flag2\":\"value2\"}'")
}

type openAuthorizer struct{}

func (openAuthorizer) Authorize(security.Context) error { return nil }

type mapDispatcher map[string]interface{}

func (d mapDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	o, ok := d[suffix]
	if !ok {
		return nil, nil, fmt.Errorf("suffix %s not found", suffix)
	}
	// TODO(caprita): Do not open authorizer even for a short-lived server.
	return o, &openAuthorizer{}, nil
}

func createServer(ctx *context.T, stderr io.Writer, objects map[string]interface{}) (string, func(), error) {
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		return "", nil, err
	}
	spec := veyron2.GetListenSpec(ctx)
	endpoints, err := server.Listen(spec)
	if err != nil {
		return "", nil, err
	}
	var name string
	if spec.Proxy != "" {
		id, err := uniqueid.Random()
		if err != nil {
			return "", nil, err
		}
		name = id.String()
	}
	if err := server.ServeDispatcher(name, mapDispatcher(objects)); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		if err := server.Stop(); err != nil {
			fmt.Fprintf(stderr, "server.Stop failed: %v", err)
		}
	}
	if name != "" {
		// Send a name rooted in our namespace root rather than the
		// relative name (in case the device manager uses a different
		// namespace root).
		//
		// TODO(caprita): Avoid relying on a mounttable altogether, and
		// instead pull out the proxied address and just send that.
		nsRoots := veyron2.GetNamespace(ctx).Roots()
		if len(nsRoots) > 0 {
			name = naming.Join(nsRoots[0], name)
		}
		return name, cleanup, nil
	}
	if len(endpoints) == 0 {
		return "", nil, fmt.Errorf("no endpoints")
	}
	return endpoints[0].Name(), cleanup, nil
}

var errNotImplemented = fmt.Errorf("method not implemented")

type binaryInvoker string

func (binaryInvoker) Create(ipc.ServerContext, int32, repository.MediaInfo) error {
	return errNotImplemented
}

func (binaryInvoker) Delete(ipc.ServerContext) error {
	return errNotImplemented
}

func (i binaryInvoker) Download(ctx repository.BinaryDownloadContext, _ int32) error {
	fileName := string(i)
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()
	bufferLength := 4096
	buffer := make([]byte, bufferLength)
	sender := ctx.SendStream()
	for {
		n, err := file.Read(buffer)
		switch err {
		case io.EOF:
			return nil
		case nil:
			if err := sender.Send(buffer[:n]); err != nil {
				return err
			}
		default:
			return err
		}
	}
}

func (binaryInvoker) DownloadURL(ipc.ServerContext) (string, int64, error) {
	return "", 0, errNotImplemented
}

func (i binaryInvoker) Stat(ctx ipc.ServerContext) ([]binary.PartInfo, repository.MediaInfo, error) {
	fileName := string(i)
	h := md5.New()
	bytes, err := ioutil.ReadFile(fileName)
	if err != nil {
		return []binary.PartInfo{}, repository.MediaInfo{}, err
	}
	h.Write(bytes)
	part := binary.PartInfo{Checksum: hex.EncodeToString(h.Sum(nil)), Size: int64(len(bytes))}
	return []binary.PartInfo{part}, packages.MediaInfoForFileName(fileName), nil
}

func (binaryInvoker) Upload(repository.BinaryUploadContext, int32) error {
	return errNotImplemented
}

func (binaryInvoker) GetACL(ctx ipc.ServerContext) (acl access.TaggedACLMap, etag string, err error) {
	return nil, "", errNotImplemented
}

func (binaryInvoker) SetACL(ctx ipc.ServerContext, acl access.TaggedACLMap, etag string) error {
	return errNotImplemented
}

type envelopeInvoker application.Envelope

func (i envelopeInvoker) Match(ipc.ServerContext, []string) (application.Envelope, error) {
	return application.Envelope(i), nil
}
func (envelopeInvoker) GetACL(ipc.ServerContext) (acl access.TaggedACLMap, etag string, err error) {
	return nil, "", errNotImplemented
}

func (envelopeInvoker) SetACL(ipc.ServerContext, access.TaggedACLMap, string) error {
	return errNotImplemented
}

// runInstallLocal creates a new envelope on the fly from the provided
// arguments, and then points the device manager back to itself for downloading
// the app envelope and binary.
//
// It sets up an app and binary server that only lives for the duration of the
// command, and listens on the profile's listen spec.  The caller should set the
// --veyron.proxy if the machine running the command is not accessible from the
// device manager.
//
// TODO(caprita/ashankar): We should use bi-directional streams to get this
// working over the same connection that the command makes to the device
// manager.
func runInstallLocal(cmd *cmdline.Command, args []string) error {
	if expectedMin, got := 2, len(args); got < expectedMin {
		return cmd.UsageErrorf("install-local: incorrect number of arguments, expected at least %d, got %d", expectedMin, got)
	}
	deviceName, title := args[0], args[1]
	args = args[2:]
	envelope := application.Envelope{Title: title}
	// Extract the environment settings, binary, and arguments.
	firstNonEnv := len(args)
	for i, arg := range args {
		if strings.Index(arg, "=") <= 0 {
			firstNonEnv = i
			break
		}
	}
	envelope.Env = args[:firstNonEnv]
	args = args[firstNonEnv:]
	if len(args) == 0 {
		return cmd.UsageErrorf("install-local: missing binary")
	}
	binary := args[0]
	args = args[1:]
	firstNonArg, firstPackage := len(args), len(args)
	for i, arg := range args {
		if arg == "PACKAGES" {
			firstNonArg = i
			firstPackage = i + 1
			break
		}
	}
	envelope.Args = args[:firstNonArg]
	pkgs := args[firstPackage:]
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("binary %v not found: %v", binary, err)
	}
	objects := map[string]interface{}{"binary": repository.BinaryServer(binaryInvoker(binary))}
	name, cancel, err := createServer(gctx, cmd.Stderr(), objects)
	if err != nil {
		return fmt.Errorf("failed to create server: %v", err)
	}
	defer cancel()
	envelope.Binary = naming.Join(name, "binary")

	// For each package dir/file specified in the arguments list, set up an
	// object in the binary service to serve that package, and add the
	// object name to the envelope's Packages map.
	var tmpZipDir string
	for _, p := range pkgs {
		if envelope.Packages == nil {
			envelope.Packages = make(map[string]string)
		}
		info, err := os.Stat(p)
		if os.IsNotExist(err) {
			return fmt.Errorf("%v not found: %v", p, err)
		} else if err != nil {
			return fmt.Errorf("Stat(%v) failed: %v", p, err)
		}
		pkgName := naming.Join("packages", info.Name())
		if _, ok := objects[pkgName]; ok {
			return fmt.Errorf("can't have more than one package with name %v", info.Name())
		}
		fileName := p
		// Directory packages first get zip'ped.
		if info.IsDir() {
			if tmpZipDir == "" {
				tmpZipDir, err = ioutil.TempDir("", "packages")
				if err != nil {
					return fmt.Errorf("failed to create a temp dir for zip packages: %v", err)
				}
				defer os.RemoveAll(tmpZipDir)
			}
			fileName = filepath.Join(tmpZipDir, info.Name()+".zip")
			if err := packages.CreateZip(fileName, p); err != nil {
				return err
			}
		}
		objects[pkgName] = repository.BinaryServer(binaryInvoker(fileName))
		envelope.Packages[info.Name()] = naming.Join(name, pkgName)
	}

	objects["application"] = repository.ApplicationServer(envelopeInvoker(envelope))
	appName := naming.Join(name, "application")
	appID, err := device.ApplicationClient(deviceName).Install(gctx, appName, device.Config(configOverride))
	// Reset the value for any future invocations of "install" or
	// "install-local" (we run more than one command per process in unit
	// tests).
	configOverride = configFlag{}
	if err != nil {
		return fmt.Errorf("Install failed: %v", err)
	}
	fmt.Fprintf(cmd.Stdout(), "Successfully installed: %q\n", naming.Join(deviceName, appID))
	return nil
}
