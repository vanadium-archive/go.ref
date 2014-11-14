package browspr

import (
	"fmt"
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
	m.count++
	name := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, m.count)
	blessing, err := m.p.BlessSelf(name)
	if err != nil {
		return security.WireBlessings{}, "", err
	}
	return security.MarshalBlessings(blessing), name, nil
}

// END MOCK BLESSER SERVICE

func setup(t *testing.T, postMessageHandler func(instanceId int32, ty string, msg string)) (*Browspr, func()) {
	spec := profiles.LocalListenSpec
	spec.Proxy = "/mock/proxy"
	browspr := NewBrowspr(postMessageHandler, spec, "/mock/identd", nil)
	browspr.accountManager.SetMockBlesser(newMockBlesserService(browspr.rt.Principal()))
	return browspr, func() {
		browspr.Shutdown()
	}
}

func TestHandleCreateAccount(t *testing.T) {
	lastInstanceId := int32(0)
	lastType := ""
	lastResponse := ""
	postMessageHandler := func(instanceId int32, ty string, response string) {
		lastInstanceId = instanceId
		lastType = ty
		lastResponse = response
	}

	browspr, teardown := setup(t, postMessageHandler)
	defer teardown()

	instanceId := int32(11) // The id of the tab / background script.
	if err := browspr.HandleCreateAccountMessage(instanceId, "mock-access-token-1"); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	if lastInstanceId != instanceId {
		t.Errorf("Received incorrect instance id on first account creation: %d", lastInstanceId)
	}
	if lastType != "createAccountResponse" {
		t.Errorf("Unexpected response type: %s", lastType)
	}
	if lastResponse != "mock-blesser/1" {
		t.Errorf("Unexpected response: %s", lastResponse)
	}

	// Verify that principalManager has the new account
	expectedAccount := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, 1)
	if b, err := browspr.accountManager.PrincipalManager().BlessingsForAccount(expectedAccount); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", expectedAccount, b, err)
	}

	if err := browspr.HandleCreateAccountMessage(instanceId, "mock-access-token-2"); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}
	if lastInstanceId != instanceId {
		t.Errorf("Received incorrect instance id on second account creation: %d", lastInstanceId)
	}
	if lastType != "createAccountResponse" {
		t.Errorf("Unexpected response type: %s", lastType)
	}
	if lastResponse != "mock-blesser/2" {
		t.Errorf("Unexpected response: %s", lastResponse)
	}

	// Verify that principalManager has the new account
	expectedAccount2 := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, 1)
	if b, err := browspr.accountManager.PrincipalManager().BlessingsForAccount(expectedAccount2); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", expectedAccount, b, err)
	}

	// Verify that principalManager has the first account
	if b, err := browspr.accountManager.PrincipalManager().BlessingsForAccount(expectedAccount); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", expectedAccount, b, err)
	}
}

func TestHandleAssocAccount(t *testing.T) {
	browspr, teardown := setup(t, nil)
	defer teardown()

	// First create an account.
	account := "mock-account"
	blessing, err := browspr.rt.Principal().BlessSelf(account)
	if err != nil {
		t.Fatalf("browspr.rt.Principal.BlessSelf(%v) failed: %v", account, err)
	}
	if err := browspr.accountManager.PrincipalManager().AddAccount(account, blessing); err != nil {
		t.Fatalf("browspr.principalManager.AddAccount(%v, %v) failed; %v", account, blessing, err)
	}

	origin := "https://my.webapp.com:443"

	// TODO(suharshs,nlacasse,bprosnitz): Get caveats here like wspr is doing. See account.go AssociateAccount.
	if err := browspr.HandleAssociateAccountMessage(origin, account, nil); err != nil {
		t.Fatalf("Failed to associate account: %v", err)
	}

	// Verify that principalManager has the correct principal for the origin
	got, err := browspr.accountManager.PrincipalManager().Principal(origin)
	if err != nil {
		t.Fatalf("browspr.principalManager.Principal(%v) failed: %v", origin, err)
	}

	if got == nil {
		t.Fatalf("Expected browspr.principalManager.Principal(%v) to return a valid principal, but got %v", origin, got)
	}
}

func TestHandleAssocAccountWithMissingAccount(t *testing.T) {
	browspr, teardown := setup(t, nil)
	defer teardown()

	account := "mock-account"
	origin := "https://my.webapp.com:443"

	// TODO(suharshs,nlacasse,bprosnitz): Get caveats here like wspr is doing. See account.go AssociateAccount.
	if err := browspr.HandleAssociateAccountMessage(origin, account, nil); err == nil {
		t.Fatalf("Expected to receive error associating non-existant account.")
	}

	// Verify that principalManager creates no principal for the origin
	got, err := browspr.accountManager.PrincipalManager().Principal(origin)
	if err == nil {
		t.Fatalf("Expected wspr.principalManager.Principal(%v) to fail, but got: %v", origin, got)
	}

	if got != nil {
		t.Fatalf("Expected wspr.principalManager.Principal(%v) not to return a principal, but got %v", origin, got)
	}
}
