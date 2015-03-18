package impl_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/mgmt/binary"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/services/mgmt/repository"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"

	binlib "v.io/x/ref/services/mgmt/lib/binary"
	pkglib "v.io/x/ref/services/mgmt/lib/packages"
)

type mockDeviceInvoker struct {
	tape *Tape
	t    *testing.T
}

// Mock ListAssociations
type ListAssociationResponse struct {
	na  []device.Association
	err error
}

func (mni *mockDeviceInvoker) ListAssociations(rpc.ServerCall) (associations []device.Association, err error) {
	vlog.VI(2).Infof("ListAssociations() was called")

	ir := mni.tape.Record("ListAssociations")
	r := ir.(ListAssociationResponse)
	return r.na, r.err
}

// Mock AssociateAccount
type AddAssociationStimulus struct {
	fun           string
	identityNames []string
	accountName   string
}

// simpleCore implements the core of all mock methods that take
// arguments and return error.
func (mni *mockDeviceInvoker) simpleCore(callRecord interface{}, name string) error {
	ri := mni.tape.Record(callRecord)
	switch r := ri.(type) {
	case nil:
		return nil
	case error:
		return r
	}
	log.Fatalf("%s (mock) response %v is of bad type", name, ri)
	return nil
}

func (mni *mockDeviceInvoker) AssociateAccount(call rpc.ServerCall, identityNames []string, accountName string) error {
	return mni.simpleCore(AddAssociationStimulus{"AssociateAccount", identityNames, accountName}, "AssociateAccount")
}

func (mni *mockDeviceInvoker) Claim(call rpc.ServerCall, pairingToken string) error {
	return mni.simpleCore("Claim", "Claim")
}

func (*mockDeviceInvoker) Describe(rpc.ServerCall) (device.Description, error) {
	return device.Description{}, nil
}

func (*mockDeviceInvoker) IsRunnable(_ rpc.ServerCall, description binary.Description) (bool, error) {
	return false, nil
}

func (*mockDeviceInvoker) Reset(call rpc.ServerCall, deadline uint64) error { return nil }

// Mock Install
type InstallStimulus struct {
	fun      string
	appName  string
	config   device.Config
	packages application.Packages
	envelope application.Envelope
	// files holds a map from file  or package name to file or package size.
	// The app binary  has the key "binary". Each of  the packages will have
	// the key "package/<package name>". The override packages will have the
	// key "overridepackage/<package name>".
	files map[string]int64
}

type InstallResponse struct {
	appId string
	err   error
}

const (
	// If provided with this app name, the mock device manager skips trying
	// to fetch the envelope from the name.
	appNameNoFetch = "skip-envelope-fetching"
	// If provided with a fetcheable app name, the mock device manager sets
	// the app name in the stimulus to this constant.
	appNameAfterFetch = "envelope-fetched"
	// The mock device manager sets the binary name in the envelope in the
	// stimulus to this constant.
	binaryNameAfterFetch = "binary-fetched"
)

func packageSize(pkgPath string) int64 {
	info, err := os.Stat(pkgPath)
	if err != nil {
		return -1
	}
	if info.IsDir() {
		infos, err := ioutil.ReadDir(pkgPath)
		if err != nil {
			return -1
		}
		var size int64
		for _, i := range infos {
			size += i.Size()
		}
		return size
	} else {
		return info.Size()
	}
}

func fetchPackageSize(ctx *context.T, pkgVON string) (int64, error) {
	dir, err := ioutil.TempDir("", "package")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp package dir: %v", err)
	}
	defer os.RemoveAll(dir)
	tmpFile := filepath.Join(dir, "downloaded")
	if err := binlib.DownloadToFile(ctx, pkgVON, tmpFile); err != nil {
		return 0, fmt.Errorf("DownloadToFile failed: %v", err)
	}
	dst := filepath.Join(dir, "install")
	if err := pkglib.Install(tmpFile, dst); err != nil {
		return 0, fmt.Errorf("packages.Install failed: %v", err)
	}
	return packageSize(dst), nil
}

func (mni *mockDeviceInvoker) Install(call rpc.ServerCall, appName string, config device.Config, packages application.Packages) (string, error) {
	is := InstallStimulus{"Install", appName, config, packages, application.Envelope{}, nil}
	if appName != appNameNoFetch {
		// Fetch the envelope and record it in the stimulus.
		envelope, err := repository.ApplicationClient(appName).Match(call.Context(), []string{"test"})
		if err != nil {
			return "", err
		}
		binaryName := envelope.Binary.File
		envelope.Binary.File = binaryNameAfterFetch
		is.appName = appNameAfterFetch
		is.files = make(map[string]int64)
		// Fetch the binary and record its size in the stimulus.
		data, mediaInfo, err := binlib.Download(call.Context(), binaryName)
		if err != nil {
			return "", err
		}
		is.files["binary"] = int64(len(data))
		if mediaInfo.Type != "application/octet-stream" {
			return "", fmt.Errorf("unexpected media type: %v", mediaInfo)
		}
		// Iterate over the packages, download them, compute the size of
		// the file(s) that make up each package, and record that in the
		// stimulus.
		for pkgLocalName, pkgVON := range envelope.Packages {
			size, err := fetchPackageSize(call.Context(), pkgVON.File)
			if err != nil {
				return "", err
			}
			is.files[naming.Join("packages", pkgLocalName)] = size
		}
		envelope.Packages = nil
		for pkgLocalName, pkg := range packages {
			size, err := fetchPackageSize(call.Context(), pkg.File)
			if err != nil {
				return "", err
			}
			is.files[naming.Join("overridepackages", pkgLocalName)] = size
		}
		is.packages = nil
		is.envelope = envelope
	}
	r := mni.tape.Record(is).(InstallResponse)
	return r.appId, r.err
}

func (*mockDeviceInvoker) Refresh(rpc.ServerCall) error { return nil }

func (*mockDeviceInvoker) Restart(rpc.ServerCall) error { return nil }

func (mni *mockDeviceInvoker) Resume(_ rpc.ServerCall) error {
	return mni.simpleCore("Resume", "Resume")
}

func (i *mockDeviceInvoker) Revert(call rpc.ServerCall) error { return nil }

type StartResponse struct {
	err  error
	msgs []device.StartServerMessage
}

func (mni *mockDeviceInvoker) Start(call rpc.StreamServerCall) error {
	ir := mni.tape.Record("Start")
	r := ir.(StartResponse)
	for _, m := range r.msgs {
		call.Send(m)
	}
	return r.err
}

type StopStimulus struct {
	fun       string
	timeDelta uint32
}

func (mni *mockDeviceInvoker) Stop(_ rpc.ServerCall, timeDelta uint32) error {
	return mni.simpleCore(StopStimulus{"Stop", timeDelta}, "Stop")
}

func (mni *mockDeviceInvoker) Suspend(_ rpc.ServerCall) error {
	return mni.simpleCore("Suspend", "Suspend")
}

func (*mockDeviceInvoker) Uninstall(rpc.ServerCall) error { return nil }

func (i *mockDeviceInvoker) Update(rpc.ServerCall) error { return nil }

func (*mockDeviceInvoker) UpdateTo(rpc.ServerCall, string) error { return nil }

// Mock AccessList getting and setting
type GetPermissionsResponse struct {
	acl  access.Permissions
	etag string
	err  error
}

type SetPermissionsStimulus struct {
	fun  string
	acl  access.Permissions
	etag string
}

func (mni *mockDeviceInvoker) SetPermissions(_ rpc.ServerCall, acl access.Permissions, etag string) error {
	return mni.simpleCore(SetPermissionsStimulus{"SetPermissions", acl, etag}, "SetPermissions")
}

func (mni *mockDeviceInvoker) GetPermissions(rpc.ServerCall) (access.Permissions, string, error) {
	ir := mni.tape.Record("GetPermissions")
	r := ir.(GetPermissionsResponse)
	return r.acl, r.etag, r.err
}

func (mni *mockDeviceInvoker) Debug(rpc.ServerCall) (string, error) {
	ir := mni.tape.Record("Debug")
	r := ir.(string)
	return r, nil
}

type dispatcher struct {
	tape *Tape
	t    *testing.T
}

func NewDispatcher(t *testing.T, tape *Tape) rpc.Dispatcher {
	return &dispatcher{tape: tape, t: t}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return &mockDeviceInvoker{tape: d.tape, t: d.t}, nil, nil
}

func startServer(t *testing.T, ctx *context.T, tape *Tape) (rpc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher(t, tape)
	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoints, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Errorf("Listen failed: %v", err)
		stopServer(t, server)
		return nil, nil, err
	}
	if err := server.ServeDispatcher("", dispatcher); err != nil {
		t.Errorf("ServeDispatcher failed: %v", err)
		stopServer(t, server)
		return nil, nil, err
	}
	return server, endpoints[0], nil
}

func stopServer(t *testing.T, server rpc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}
