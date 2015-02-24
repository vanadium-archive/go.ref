package server

import (
	"fmt"
	"reflect"
	"testing"

	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	"v.io/v23/vdl/vdlroot/src/signature"
	"v.io/wspr/veyron/services/wsprd/lib"
	"v.io/wspr/veyron/services/wsprd/lib/testwriter"
)

func init() {
	EnableCustomWsprValidator = true
}

type mockFlowFactory struct {
	writer testwriter.Writer
}

func (m *mockFlowFactory) createFlow() *Flow {
	return &Flow{ID: 0, Writer: &m.writer}
}

func (*mockFlowFactory) cleanupFlow(int32) {}

type mockInvoker struct {
	handle     int32
	sig        []signature.Interface
	hasGlobber bool
}

func (m mockInvoker) Prepare(string, int) ([]interface{}, []*vdl.Value, error) {
	return nil, nil, nil
}

func (mockInvoker) Invoke(string, ipc.ServerCall, []interface{}) ([]interface{}, error) {
	return nil, nil
}

func (mockInvoker) Globber() *ipc.GlobState {
	return nil
}

func (m mockInvoker) Signature(ctx ipc.ServerContext) ([]signature.Interface, error) {
	return m.sig, nil
}

func (m mockInvoker) MethodSignature(ctx ipc.ServerContext, methodName string) (signature.Method, error) {
	method, found := m.sig[0].FindMethod(methodName)
	if !found {
		return signature.Method{}, fmt.Errorf("Method %q not found", methodName)
	}
	return method, nil
}

type mockInvokerFactory struct{}

func (mockInvokerFactory) createInvoker(handle int32, sig []signature.Interface, hasGlobber bool) (ipc.Invoker, error) {
	return &mockInvoker{handle: handle, sig: sig, hasGlobber: hasGlobber}, nil
}

type mockAuthorizer struct {
	handle        int32
	hasAuthorizer bool
}

func (mockAuthorizer) Authorize(security.Context) error { return nil }

type mockAuthorizerFactory struct{}

func (mockAuthorizerFactory) createAuthorizer(handle int32, hasAuthorizer bool) (security.Authorizer, error) {
	return mockAuthorizer{handle: handle, hasAuthorizer: hasAuthorizer}, nil
}

func TestSuccessfulLookup(t *testing.T) {
	flowFactory := &mockFlowFactory{}
	d := newDispatcher(0, flowFactory, mockInvokerFactory{}, mockAuthorizerFactory{})
	expectedSig := []signature.Interface{
		{Name: "AName"},
	}
	go func() {
		if err := flowFactory.writer.WaitForMessage(1); err != nil {
			t.Errorf("failed to get dispatch request %v", err)
			t.Fail()
		}
		jsonResponse := fmt.Sprintf(`{"handle":1,"hasAuthorizer":false,"signature":"%s"}`, lib.VomEncodeOrDie(expectedSig))
		d.handleLookupResponse(0, jsonResponse)
	}()

	invoker, auth, err := d.Lookup("a/b")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedInvoker := &mockInvoker{handle: 1, sig: expectedSig}
	if !reflect.DeepEqual(invoker, expectedInvoker) {
		t.Errorf("wrong invoker returned, expected: %#v, got :%#v", expectedInvoker, invoker)
	}

	expectedAuth := mockAuthorizer{handle: 1, hasAuthorizer: false}
	if !reflect.DeepEqual(auth, expectedAuth) {
		t.Errorf("wrong authorizer returned, expected: %v, got :%v", expectedAuth, auth)
	}

	expectedResponses := []lib.Response{
		{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "a/b",
			},
		},
	}
	if err := testwriter.CheckResponses(&flowFactory.writer, expectedResponses, nil); err != nil {
		t.Error(err)
	}
}

func TestSuccessfulLookupWithAuthorizer(t *testing.T) {
	flowFactory := &mockFlowFactory{}
	d := newDispatcher(0, flowFactory, mockInvokerFactory{}, mockAuthorizerFactory{})
	expectedSig := []signature.Interface{
		{Name: "AName"},
	}
	go func() {
		if err := flowFactory.writer.WaitForMessage(1); err != nil {
			t.Errorf("failed to get dispatch request %v", err)
			t.Fail()
		}
		jsonResponse := fmt.Sprintf(`{"handle":1,"hasAuthorizer":true,"signature":"%s"}`, lib.VomEncodeOrDie(expectedSig))
		d.handleLookupResponse(0, jsonResponse)
	}()

	invoker, auth, err := d.Lookup("a/b")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedInvoker := &mockInvoker{handle: 1, sig: expectedSig}
	if !reflect.DeepEqual(invoker, expectedInvoker) {
		t.Errorf("wrong invoker returned, expected: %v, got :%v", expectedInvoker, invoker)
	}

	expectedAuth := mockAuthorizer{handle: 1, hasAuthorizer: true}
	if !reflect.DeepEqual(auth, expectedAuth) {
		t.Errorf("wrong authorizer returned, expected: %v, got :%v", expectedAuth, auth)
	}

	expectedResponses := []lib.Response{
		{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "a/b",
			},
		},
	}
	if err := testwriter.CheckResponses(&flowFactory.writer, expectedResponses, nil); err != nil {
		t.Error(err)
	}
}

func TestFailedLookup(t *testing.T) {
	flowFactory := &mockFlowFactory{}
	d := newDispatcher(0, flowFactory, mockInvokerFactory{}, mockAuthorizerFactory{})
	go func() {
		if err := flowFactory.writer.WaitForMessage(1); err != nil {
			t.Errorf("failed to get dispatch request %v", err)
			t.Fail()
		}
		jsonResponse := `{"err":{"id":"v23/verror.Exists","msg":"bad stuff"}}`
		d.handleLookupResponse(0, jsonResponse)
	}()

	_, _, err := d.Lookup("a/b")

	if err == nil {
		t.Errorf("expected error, but got none", err)
	}

	expectedResponses := []lib.Response{
		{
			Type: lib.ResponseDispatcherLookup,
			Message: map[string]interface{}{
				"serverId": 0.0,
				"suffix":   "a/b",
			},
		},
	}
	if err := testwriter.CheckResponses(&flowFactory.writer, expectedResponses, nil); err != nil {
		t.Error(err)
	}
}
