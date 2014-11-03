package server

import (
	"fmt"
	"reflect"
	"testing"

	_ "veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/lib/testwriter"
	"veyron.io/wspr/veyron/services/wsprd/signature"
)

type mockFlowFactory struct {
	writer testwriter.Writer
}

func (m *mockFlowFactory) createFlow() *Flow {
	return &Flow{ID: 0, Writer: &m.writer}
}

func (*mockFlowFactory) cleanupFlow(int64) {}

type mockInvoker struct {
	handle int64
	sig    signature.JSONServiceSignature
	label  security.Label
}

func (m mockInvoker) Prepare(string, int) ([]interface{}, []interface{}, error) {
	return nil, []interface{}{m.label}, nil
}

func (mockInvoker) Invoke(string, ipc.ServerCall, []interface{}) ([]interface{}, error) {
	return nil, nil
}

type mockInvokerFactory struct{}

func (mockInvokerFactory) createInvoker(handle int64, sig signature.JSONServiceSignature, label security.Label) (ipc.Invoker, error) {
	return &mockInvoker{handle: handle, sig: sig, label: label}, nil
}

type mockAuthorizer struct {
	handle        int64
	hasAuthorizer bool
}

func (mockAuthorizer) Authorize(security.Context) error { return nil }

type mockAuthorizerFactory struct{}

func (mockAuthorizerFactory) createAuthorizer(handle int64, hasAuthorizer bool) (security.Authorizer, error) {
	return mockAuthorizer{handle: handle, hasAuthorizer: hasAuthorizer}, nil
}

func init() {
	rt.Init()
}

func TestSuccessfulLookup(t *testing.T) {
	flowFactory := &mockFlowFactory{}
	d := newDispatcher(0, flowFactory, mockInvokerFactory{}, mockAuthorizerFactory{}, rt.R().Logger())
	go func() {
		if err := flowFactory.writer.WaitForMessage(1); err != nil {
			t.Errorf("failed to get dispatch request %v", err)
			t.Fail()
		}
		signature := `{"add":{"inArgs":["foo","bar"],"numOutArgs":1,"isStreaming":false}}`
		jsonResponse := fmt.Sprintf(`{"handle":1,"hasAuthorizer":false,"label":%d,"signature":%s}`, security.WriteLabel, signature)
		d.handleLookupResponse(0, jsonResponse)
	}()

	invoker, auth, err := d.Lookup("a/b", "Read")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedSig := signature.JSONServiceSignature{
		"add": signature.JSONMethodSignature{
			InArgs:     []string{"foo", "bar"},
			NumOutArgs: 1,
		},
	}
	expectedInvoker := &mockInvoker{handle: 1, sig: expectedSig, label: security.WriteLabel}
	if !reflect.DeepEqual(invoker, expectedInvoker) {
		t.Errorf("wrong invoker returned, expected: %v, got :%v", expectedInvoker, invoker)
	}

	expectedAuth := mockAuthorizer{handle: 1, hasAuthorizer: false}
	if !reflect.DeepEqual(auth, expectedAuth) {
		t.Errorf("wrong authorizer returned, expected: %v, got :%v", expectedAuth, auth)
	}

	expectedResponses := []testwriter.Response{
		testwriter.Response{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "a/b",
				"method":   "read",
			},
		},
	}
	if err := testwriter.CheckResponses(&flowFactory.writer, expectedResponses, nil); err != nil {
		t.Error(err)
	}
}

func TestSuccessfulLookupWithAuthorizer(t *testing.T) {
	flowFactory := &mockFlowFactory{}
	d := newDispatcher(0, flowFactory, mockInvokerFactory{}, mockAuthorizerFactory{}, rt.R().Logger())
	go func() {
		if err := flowFactory.writer.WaitForMessage(1); err != nil {
			t.Errorf("failed to get dispatch request %v", err)
			t.Fail()
		}
		signature := `{"add":{"inArgs":["foo","bar"],"numOutArgs":1,"isStreaming":false}}`
		jsonResponse := fmt.Sprintf(`{"handle":1,"hasAuthorizer":true,"label":%d,"signature":%s}`, security.ReadLabel, signature)
		d.handleLookupResponse(0, jsonResponse)
	}()

	invoker, auth, err := d.Lookup("a/b", "Read")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedSig := signature.JSONServiceSignature{
		"add": signature.JSONMethodSignature{
			InArgs:     []string{"foo", "bar"},
			NumOutArgs: 1,
		},
	}
	expectedInvoker := &mockInvoker{handle: 1, sig: expectedSig, label: security.ReadLabel}
	if !reflect.DeepEqual(invoker, expectedInvoker) {
		t.Errorf("wrong invoker returned, expected: %v, got :%v", expectedInvoker, invoker)
	}

	expectedAuth := mockAuthorizer{handle: 1, hasAuthorizer: true}
	if !reflect.DeepEqual(auth, expectedAuth) {
		t.Errorf("wrong authorizer returned, expected: %v, got :%v", expectedAuth, auth)
	}

	expectedResponses := []testwriter.Response{
		testwriter.Response{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "a/b",
				"method":   "read",
			},
		},
	}
	if err := testwriter.CheckResponses(&flowFactory.writer, expectedResponses, nil); err != nil {
		t.Error(err)
	}
}

func TestFailedLookup(t *testing.T) {
	flowFactory := &mockFlowFactory{}
	d := newDispatcher(0, flowFactory, mockInvokerFactory{}, mockAuthorizerFactory{}, rt.R().Logger())
	go func() {
		if err := flowFactory.writer.WaitForMessage(1); err != nil {
			t.Errorf("failed to get dispatch request %v", err)
			t.Fail()
		}
		jsonResponse := `{"err":{"id":"veyron2/verror.Exists","msg":"bad stuff"}}`
		d.handleLookupResponse(0, jsonResponse)
	}()

	_, _, err := d.Lookup("a/b", "Read")

	if err == nil {
		t.Errorf("expected error, but got none", err)
	}

	expectedResponses := []testwriter.Response{
		testwriter.Response{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "a/b",
				"method":   "read",
			},
		},
	}
	if err := testwriter.CheckResponses(&flowFactory.writer, expectedResponses, nil); err != nil {
		t.Error(err)
	}
}
