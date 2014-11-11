package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/binary"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/services/mounttable"

	"veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"
)

type mockNodeInvoker struct {
	tape *Tape
	t    *testing.T
}

type ListAssociationResponse struct {
	na  []node.Association
	err error
}

func (mni *mockNodeInvoker) ListAssociations(ipc.ServerContext) (associations []node.Association, err error) {
	vlog.VI(2).Infof("ListAssociations() was called")

	ir := mni.tape.Record("ListAssociations")
	r := ir.(ListAssociationResponse)
	return r.na, r.err
}

type AddAssociationStimulus struct {
	fun           string
	identityNames []string
	accountName   string
}

func (i *mockNodeInvoker) AssociateAccount(call ipc.ServerContext, identityNames []string, accountName string) error {
	ri := i.tape.Record(AddAssociationStimulus{"AssociateAccount", identityNames, accountName})
	switch r := ri.(type) {
	case nil:
		return nil
	case error:
		return r
	}
	i.t.Fatalf("AssociateAccount (mock) response %v is of bad type", ri)
	return nil
}

func (i *mockNodeInvoker) Claim(call ipc.ServerContext) error { return nil }
func (*mockNodeInvoker) Describe(ipc.ServerContext) (node.Description, error) {
	return node.Description{}, nil
}
func (*mockNodeInvoker) IsRunnable(_ ipc.ServerContext, description binary.Description) (bool, error) {
	return false, nil
}
func (*mockNodeInvoker) Reset(call ipc.ServerContext, deadline uint64) error    { return nil }
func (*mockNodeInvoker) Install(ipc.ServerContext, string) (string, error)      { return "", nil }
func (*mockNodeInvoker) Refresh(ipc.ServerContext) error                        { return nil }
func (*mockNodeInvoker) Restart(ipc.ServerContext) error                        { return nil }
func (*mockNodeInvoker) Resume(ipc.ServerContext) error                         { return nil }
func (i *mockNodeInvoker) Revert(call ipc.ServerContext) error                  { return nil }
func (*mockNodeInvoker) Start(ipc.ServerContext) ([]string, error)              { return []string{}, nil }
func (*mockNodeInvoker) Stop(ipc.ServerContext, uint32) error                   { return nil }
func (*mockNodeInvoker) Suspend(ipc.ServerContext) error                        { return nil }
func (*mockNodeInvoker) Uninstall(ipc.ServerContext) error                      { return nil }
func (i *mockNodeInvoker) Update(ipc.ServerContext) error                       { return nil }
func (*mockNodeInvoker) UpdateTo(ipc.ServerContext, string) error               { return nil }
func (i *mockNodeInvoker) SetACL(ipc.ServerContext, security.ACL, string) error { return nil }
func (i *mockNodeInvoker) GetACL(ipc.ServerContext) (security.ACL, string, error) {
	return security.ACL{}, "", nil
}
func (i *mockNodeInvoker) Glob(ctx mounttable.GlobbableGlobContext, pattern string) error {
	return nil
}

type dispatcher struct {
	tape *Tape
	t    *testing.T
}

func NewDispatcher(t *testing.T, tape *Tape) *dispatcher {
	return &dispatcher{tape: tape, t: t}
}

func (d *dispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(node.NodeServer(&mockNodeInvoker{tape: d.tape, t: d.t}))
	return invoker, nil, nil
}

func startServer(t *testing.T, r veyron2.Runtime, tape *Tape) (ipc.Server, naming.Endpoint, error) {
	dispatcher := NewDispatcher(t, tape)
	server, err := r.NewServer()
	if err != nil {
		t.Errorf("NewServer failed: %v", err)
		return nil, nil, err
	}
	endpoint, err := server.Listen(profiles.LocalListenSpec)
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
	return server, endpoint, nil
}

func stopServer(t *testing.T, server ipc.Server) {
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}

func TestListCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

	// Test the 'list' command.
	tape.SetResponses([]interface{}{ListAssociationResponse{
		na: []node.Association{
			{
				"root/self",
				"alice_self_account",
			},
			{
				"root/other",
				"alice_other_account",
			},
		},
		err: nil,
	}})

	if err := cmd.Execute([]string{"associate", "list", nodeName}); err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := "root/self alice_self_account\nroot/other alice_other_account", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if got, expected := tape.Play(), []interface{}{"ListAssociations"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	// Test list with bad parameters.
	if err := cmd.Execute([]string{"associate", "list", nodeName, "hello"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestAddCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "//myapp/1")

	if err := cmd.Execute([]string{"add", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "add", nodeName, "alice", "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected := []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/self"}, "alice"},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "add", nodeName, "alice", "root/other", "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected = []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/other", "root/self"}, "alice"},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestRemoveCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "//myapp/1")

	if err := cmd.Execute([]string{"remove", "one"}); err == nil {
		t.Fatalf("wrongly failed to receive a non-nil error.")
	}
	if got, expected := len(tape.Play()), 0; got != expected {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()

	tape.SetResponses([]interface{}{nil})
	if err := cmd.Execute([]string{"associate", "remove", nodeName, "root/self"}); err != nil {
		t.Fatalf("%v", err)
	}
	expected := []interface{}{
		AddAssociationStimulus{"AssociateAccount", []string{"root/self"}, ""},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("unexpected result. Got %v want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}
