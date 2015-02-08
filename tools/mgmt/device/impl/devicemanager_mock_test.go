package impl_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

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
	"v.io/core/veyron2/vlog"

	binlib "v.io/core/veyron/services/mgmt/lib/binary"
	"v.io/core/veyron/services/mgmt/lib/packages"
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

func (mni *mockDeviceInvoker) ListAssociations(ipc.ServerContext) (associations []device.Association, err error) {
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

func (mni *mockDeviceInvoker) AssociateAccount(call ipc.ServerContext, identityNames []string, accountName string) error {
	return mni.simpleCore(AddAssociationStimulus{"AssociateAccount", identityNames, accountName}, "AssociateAccount")
}

func (mni *mockDeviceInvoker) Claim(call ipc.ServerContext) error {
	return mni.simpleCore("Claim", "Claim")
}

func (*mockDeviceInvoker) Describe(ipc.ServerContext) (device.Description, error) {
	return device.Description{}, nil
}

func (*mockDeviceInvoker) IsRunnable(_ ipc.ServerContext, description binary.Description) (bool, error) {
	return false, nil
}

func (*mockDeviceInvoker) Reset(call ipc.ServerContext, deadline uint64) error { return nil }

// Mock Install
type InstallStimulus struct {
	fun      string
	appName  string
	config   device.Config
	envelope application.Envelope
	// files holds a map from file or package name to file or package size.
	// The app binary has the key "binary". Each of the packages will have
	// the key "package/<package name>".
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

func (mni *mockDeviceInvoker) Install(call ipc.ServerContext, appName string, config device.Config) (string, error) {
	is := InstallStimulus{"Install", appName, config, application.Envelope{}, nil}
	if appName != appNameNoFetch {
		// Fetch the envelope and record it in the stimulus.
		envelope, err := repository.ApplicationClient(appName).Match(call.Context(), []string{"test"})
		if err != nil {
			return "", err
		}
		binaryName := envelope.Binary
		envelope.Binary = binaryNameAfterFetch
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
			dir, err := ioutil.TempDir("", "package")
			if err != nil {
				return "", fmt.Errorf("failed to create temp package dir: %v", err)
			}
			defer os.RemoveAll(dir)
			tmpFile := filepath.Join(dir, pkgLocalName)
			if err := binlib.DownloadToFile(call.Context(), pkgVON, tmpFile); err != nil {
				return "", fmt.Errorf("DownloadToFile failed: %v", err)
			}
			dst := filepath.Join(dir, "install")
			if err := packages.Install(tmpFile, dst); err != nil {
				return "", fmt.Errorf("packages.Install failed: %v", err)
			}
			is.files[naming.Join("packages", pkgLocalName)] = packageSize(dst)
		}
		envelope.Packages = nil
		is.envelope = envelope
	}
	r := mni.tape.Record(is).(InstallResponse)
	return r.appId, r.err
}

func (*mockDeviceInvoker) Refresh(ipc.ServerContext) error { return nil }

func (*mockDeviceInvoker) Restart(ipc.ServerContext) error { return nil }

func (mni *mockDeviceInvoker) Resume(_ ipc.ServerContext) error {
	return mni.simpleCore("Resume", "Resume")
}

func (i *mockDeviceInvoker) Revert(call ipc.ServerContext) error { return nil }

type StartResponse struct {
	appIds []string
	err    error
}

func (mni *mockDeviceInvoker) Start(ipc.ServerContext) ([]string, error) {
	ir := mni.tape.Record("Start")
	r := ir.(StartResponse)
	return r.appIds, r.err
}

type StopStimulus struct {
	fun       string
	timeDelta uint32
}

func (mni *mockDeviceInvoker) Stop(_ ipc.ServerContext, timeDelta uint32) error {
	return mni.simpleCore(StopStimulus{"Stop", timeDelta}, "Stop")
}

func (mni *mockDeviceInvoker) Suspend(_ ipc.ServerContext) error {
	return mni.simpleCore("Suspend", "Suspend")
}

func (*mockDeviceInvoker) Uninstall(ipc.ServerContext) error { return nil }

func (i *mockDeviceInvoker) Update(ipc.ServerContext) error { return nil }

func (*mockDeviceInvoker) UpdateTo(ipc.ServerContext, string) error { return nil }

// Mock ACL getting and setting
type GetACLResponse struct {
	acl  access.TaggedACLMap
	etag string
	err  error
}

type SetACLStimulus struct {
	fun  string
	acl  access.TaggedACLMap
	etag string
}

func (mni *mockDeviceInvoker) SetACL(_ ipc.ServerContext, acl access.TaggedACLMap, etag string) error {
	return mni.simpleCore(SetACLStimulus{"SetACL", acl, etag}, "SetACL")
}

func (mni *mockDeviceInvoker) GetACL(ipc.ServerContext) (access.TaggedACLMap, string, error) {
	ir := mni.tape.Record("GetACL")
	r := ir.(GetACLResponse)
	return r.acl, r.etag, r.err
}

func (mni *mockDeviceInvoker) Debug(ipc.ServerContext) (string, error) {
	ir := mni.tape.Record("Debug")
	r := ir.(string)
	return r, nil
}

type dispatcher struct {
	tape *Tape
	t    *testing.T
}

func NewDispatcher(t *testing.T, tape *Tape) ipc.Dispatcher {
	return &dispatcher{tape: tape, t: t}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return device.DeviceServer(&mockDeviceInvoker{tape: d.tape, t: d.t}), nil, nil
}

func startServer(t *testing.T, ctx *context.T, tape *Tape) (ipc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher(t, tape)
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoints, err := server.Listen(veyron2.GetListenSpec(ctx))
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

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}
