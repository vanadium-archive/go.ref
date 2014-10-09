package wspr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"

	"veyron.io/veyron/veyron/profiles"
)

// BEGIN MOCK BLESSER SERVICE
// TODO(nlacasse): Is there a better way to mock this?!
type mockBlesserService struct {
	id    security.PrivateID
	count int
}

func newMockBlesserService(id security.PrivateID) *mockBlesserService {
	return &mockBlesserService{
		id:    id,
		count: 0,
	}
}

func (m *mockBlesserService) BlessUsingAccessToken(c context.T, accessToken string, co ...ipc.CallOpt) (vdlutil.Any, string, error) {
	m.count = m.count + 1
	name := fmt.Sprintf("mock-blessing-%v", m.count)
	blessing, err := m.id.Bless(m.id.PublicID(), name, 5*time.Minute, nil)
	if err != nil {
		return nil, "", err
	}
	return blessing, name, nil
}

// This is never used.  Only needed for mock.
func (m *mockBlesserService) GetMethodTags(c context.T, s string, co ...ipc.CallOpt) ([]interface{}, error) {
	return nil, nil
}

// This is never used.  Only needed for mock.
func (m *mockBlesserService) Signature(c context.T, co ...ipc.CallOpt) (ipc.ServiceSignature, error) {
	return ipc.ServiceSignature{}, nil
}

// This is never used.  Only needed for mock.
func (m *mockBlesserService) UnresolveStep(c context.T, co ...ipc.CallOpt) ([]string, error) {
	return []string{}, nil
}

// END MOCK BLESSER SERVICE

func setup(t *testing.T) (*WSPR, func()) {
	spec := *profiles.LocalListenSpec
	spec.Proxy = "/mock/proxy"
	wspr := NewWSPR(0, spec, "/mock/identd")
	providerId := wspr.rt.Identity()

	wspr.blesserService = newMockBlesserService(providerId)
	return wspr, func() {
		wspr.Shutdown()
	}
}

func TestHandleCreateAccount(t *testing.T) {
	wspr, teardown := setup(t)
	defer teardown()

	method := "POST"
	path := "/create-account"

	// Add one account
	data1 := createAccountInput{
		AccessToken: "mock-access-token-1",
	}
	data1Json, err := json.Marshal(data1)
	if err != nil {
		t.Fatalf("json.Marshal(%v) failed: %v", data1, err)
	}

	data1JsonReader := bytes.NewReader(data1Json)
	req, err := http.NewRequest(method, path, (data1JsonReader))
	if err != nil {
		t.Fatalf("http.NewRequest(%v, %v, %v,) failed: %v", method, path, data1JsonReader, err)
	}

	resp1 := httptest.NewRecorder()
	wspr.handleCreateAccount(resp1, req)
	if resp1.Code != 200 {
		t.Fatalf("Expected handleCreateAccount to return 200 OK, instead got %v", resp1)
	}

	// Verify that idManager has the new account
	topLevelName := wspr.rt.Identity().PublicID().Names()[0]
	expectedAccountName := topLevelName + "/mock-blessing-1"
	gotAccounts := wspr.idManager.AccountsMatching(security.BlessingPattern(expectedAccountName))
	if len(gotAccounts) != 1 {
		t.Fatalf("Expected to have 1 account with name %v, but got %v: %v", expectedAccountName, len(gotAccounts), gotAccounts)
	}

	// Add another account
	data2 := createAccountInput{
		AccessToken: "mock-access-token-2",
	}
	data2Json, err := json.Marshal(data2)
	if err != nil {
		t.Fatalf("json.Marshal(%v) failed: %v", data2, err)
	}
	data2JsonReader := bytes.NewReader(data2Json)
	req, err = http.NewRequest(method, path, data2JsonReader)
	if err != nil {
		t.Fatalf("http.NewRequest(%v, %v, %v,) failed: %v", method, path, data2JsonReader, err)
	}

	resp2 := httptest.NewRecorder()
	wspr.handleCreateAccount(resp2, req)
	if resp2.Code != 200 {
		t.Fatalf("Expected handleCreateAccount to return 200 OK, instead got %v", resp2)
	}

	// Verify that idManager has both accounts
	gotAccounts = wspr.idManager.AccountsMatching(security.BlessingPattern(fmt.Sprintf("%s%s%v", topLevelName, security.ChainSeparator, security.AllPrincipals)))
	if len(gotAccounts) != 2 {
		t.Fatalf("Expected to have 2 accounts, but got %v: %v", len(gotAccounts), gotAccounts)
	}
}

func TestHandleAssocAccount(t *testing.T) {
	wspr, teardown := setup(t)
	defer teardown()

	// First create an accounts.
	accountName := "mock-account"
	identityName := "mock-id"
	privateID, err := wspr.rt.NewIdentity(identityName)
	if err != nil {
		t.Fatalf("wspr.rt.NewIdentity(%v) failed: %v", identityName, err)
	}
	if err := wspr.idManager.AddAccount(accountName, privateID); err != nil {
		t.Fatalf("wspr.idManager.AddAccount(%v, %v) failed; %v", accountName, privateID, err)
	}

	// Associate with that account
	method := "POST"
	path := "/assoc-account"

	origin := "https://my.webapp.com:443"
	data := assocAccountInput{
		Name:   accountName,
		Origin: origin,
	}

	dataJson, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal(%v) failed: %v", data, err)
	}

	dataJsonReader := bytes.NewReader(dataJson)
	req, err := http.NewRequest(method, path, (dataJsonReader))
	if err != nil {
		t.Fatalf("http.NewRequest(%v, %v, %v,) failed: %v", method, path, dataJsonReader, err)
	}

	resp := httptest.NewRecorder()
	wspr.handleAssocAccount(resp, req)
	if resp.Code != 200 {
		t.Fatalf("Expected handleAssocAccount to return 200 OK, instead got %v", resp)
	}

	// Verify that idManager has the correct identity for the origin
	gotID, err := wspr.idManager.Identity(origin)
	if err != nil {
		t.Fatalf("wspr.idManager.Identity(%v) failed: %v", origin, err)
	}

	if gotID == nil {
		t.Fatalf("Expected wspr.idManager.Identity(%v) to return an valid identity, but got %v", origin, gotID)
	}
}

func TestHandleAssocAccountWithMissingAccount(t *testing.T) {
	wspr, teardown := setup(t)
	defer teardown()

	method := "POST"
	path := "/assoc-account"

	accountName := "mock-account"
	origin := "https://my.webapp.com:443"
	data := assocAccountInput{
		Name:   accountName,
		Origin: origin,
	}

	dataJson, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal(%v) failed: %v", data, err)
	}

	dataJsonReader := bytes.NewReader(dataJson)
	req, err := http.NewRequest(method, path, (dataJsonReader))
	if err != nil {
		t.Fatalf("http.NewRequest(%v, %v, %v,) failed: %v", method, path, dataJsonReader, err)
	}

	// Verify that the request fails with 400 Bad Request error
	resp := httptest.NewRecorder()
	wspr.handleAssocAccount(resp, req)
	if resp.Code != 400 {
		t.Fatalf("Expected handleAssocAccount to return 400 error, but got %v", resp)
	}

	// Verify that idManager has no identities for the origin
	gotID, err := wspr.idManager.Identity(origin)
	if err == nil {
		t.Fatalf("Expected wspr.idManager.Identity(%v) to fail, but got: %v", origin, gotID)
	}

	if gotID != nil {
		t.Fatalf("Expected wspr.idManager.Identity(%v) not to return an identity, but got %v", origin, gotID)
	}
}
