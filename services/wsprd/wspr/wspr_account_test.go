package wspr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"

	"veyron.io/veyron/veyron/profiles"
)

const topLevelName = "mock-blesser"

// BEGIN MOCK BLESSER SERVICE
// TODO(nlacasse): Is there a better way to mock this?!
type mockBlesserService struct {
	p     security.Principal
	count int
}

func newMockBlesserService(p security.Principal) *mockBlesserService {
	return &mockBlesserService{
		p:     p,
		count: 0,
	}
}

func (m *mockBlesserService) BlessUsingAccessToken(c context.T, accessToken string, co ...ipc.CallOpt) (security.WireBlessings, string, error) {
	var empty security.WireBlessings
	m.count++
	name := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, m.count)
	blessing, err := m.p.BlessSelf(name)
	if err != nil {
		return empty, "", err
	}
	return security.MarshalBlessings(blessing), name, nil
}

// END MOCK BLESSER SERVICE

func setup(t *testing.T) (*WSPR, func()) {
	spec := profiles.LocalListenSpec
	spec.Proxy = "/mock/proxy"
	wspr := NewWSPR(0, nil, &spec, "/mock/identd", nil)
	wspr.accountManager.SetMockBlesser(newMockBlesserService(wspr.rt.Principal()))
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

	// Verify that principalManager has the new account
	account1 := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, 1)
	if b, err := wspr.principalManager.BlessingsForAccount(account1); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account1, b, err)
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

	// Verify that principalManager has both accounts
	if b, err := wspr.principalManager.BlessingsForAccount(account1); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account1, b, err)
	}
	account2 := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, 2)
	if b, err := wspr.principalManager.BlessingsForAccount(account2); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account2, b, err)
	}
}

func TestHandleAssocAccount(t *testing.T) {
	wspr, teardown := setup(t)
	defer teardown()

	// First create an account.
	account := "mock-account"
	blessing, err := wspr.rt.Principal().BlessSelf(account)
	if err != nil {
		t.Fatalf("wspr.rt.Principal.BlessSelf(%v) failed: %v", account, err)
	}
	if err := wspr.principalManager.AddAccount(account, blessing); err != nil {
		t.Fatalf("wspr.principalManager.AddAccount(%v, %v) failed; %v", account, blessing, err)
	}

	// Associate with that account
	method := "POST"
	path := "/assoc-account"

	origin := "https://my.webapp.com:443"
	data := assocAccountInput{
		Account: account,
		Origin:  origin,
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

	// Verify that principalManager has the correct principal for the origin
	got, err := wspr.principalManager.Principal(origin)
	if err != nil {
		t.Fatalf("wspr.principalManager.Principal(%v) failed: %v", origin, err)
	}

	if got == nil {
		t.Fatalf("Expected wspr.principalManager.Principal(%v) to return a valid principal, but got %v", origin, got)
	}
}

func TestHandleAssocAccountWithMissingAccount(t *testing.T) {
	wspr, teardown := setup(t)
	defer teardown()

	method := "POST"
	path := "/assoc-account"

	account := "mock-account"
	origin := "https://my.webapp.com:443"
	data := assocAccountInput{
		Account: account,
		Origin:  origin,
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

	// Verify that principalManager creates no principal for the origin
	got, err := wspr.principalManager.Principal(origin)
	if err == nil {
		t.Fatalf("Expected wspr.principalManager.Principal(%v) to fail, but got: %v", origin, got)
	}

	if got != nil {
		t.Fatalf("Expected wspr.principalManager.Principal(%v) not to return a principal, but got %v", origin, got)
	}
}
