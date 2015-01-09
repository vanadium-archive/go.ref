package browspr

import (
	"fmt"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/valconv"

	"v.io/core/veyron/profiles"
)

const topLevelName = "mock-blesser"

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

func (m *mockBlesserService) BlessUsingAccessToken(c *context.T, accessToken string, co ...ipc.CallOpt) (security.WireBlessings, string, error) {
	var empty security.WireBlessings
	m.count++
	name := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, m.count)
	blessing, err := m.p.BlessSelf(name)
	if err != nil {
		return empty, "", err
	}
	return security.MarshalBlessings(blessing), name, nil
}

func setup(t *testing.T) (*Browspr, func()) {
	spec := profiles.LocalListenSpec
	spec.Proxy = "/mock/proxy"
	r, err := rt.New()
	if err != nil {
		t.Fatal(err)
	}
	mockPostMessage := func(_ int32, _, _ string) {}
	browspr := NewBrowspr(r.NewContext(), mockPostMessage, nil, &spec, "/mock:1234/identd", nil)
	principal := veyron2.GetPrincipal(browspr.ctx)
	browspr.accountManager.SetMockBlesser(newMockBlesserService(principal))

	return browspr, func() {
		browspr.Shutdown()
		r.Cleanup()
	}
}

func TestHandleCreateAccount(t *testing.T) {
	browspr, teardown := setup(t)
	defer teardown()

	// Add one account
	message1, err := valueOf(createAccountMessage{
		Token: "mock-access-token-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	account1, err := browspr.HandleAuthCreateAccountRpc(message1)
	if err != nil {
		t.Fatalf("browspr.HandleAuthCreateAccountRpc(%v) failed: %v", message1, err)
	}

	// Verify that principalManager has the new account
	if b, err := browspr.principalManager.BlessingsForAccount(account1.(string)); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account1, b, err)
	}

	// Add another account
	message2, err := valueOf(createAccountMessage{
		Token: "mock-access-token-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	account2, err := browspr.HandleAuthCreateAccountRpc(message2)
	if err != nil {
		t.Fatalf("browspr.HandleAuthCreateAccountRpc(%v) failed: %v", message2, err)
	}

	// Verify that principalManager has both accounts
	if b, err := browspr.principalManager.BlessingsForAccount(account1.(string)); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account1, b, err)
	}
	if b, err := browspr.principalManager.BlessingsForAccount(account2.(string)); err != nil || b == nil {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account2, b, err)
	}
}

func TestHandleAssocAccount(t *testing.T) {
	browspr, teardown := setup(t)
	defer teardown()

	// First create an account.
	account := "mock-account"
	principal := veyron2.GetPrincipal(browspr.ctx)
	blessing, err := principal.BlessSelf(account)
	if err != nil {
		t.Fatalf("browspr.rt.Principal.BlessSelf(%v) failed: %v", account, err)
	}
	if err := browspr.principalManager.AddAccount(account, blessing); err != nil {
		t.Fatalf("browspr.principalManager.AddAccount(%v, %v) failed; %v", account, blessing, err)
	}

	origin := "https://my.webapp.com:443"
	message, err := valueOf(associateAccountMessage{
		Account: account,
		Origin:  origin,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := browspr.HandleAuthAssociateAccountRpc(message); err != nil {
		t.Fatalf("browspr.HandleAuthAssociateAccountRpc(%v) failed: %v", message, err)
	}

	// Verify that principalManager has the correct principal for the origin
	got, err := browspr.principalManager.Principal(origin)
	if err != nil {
		t.Fatalf("browspr.principalManager.Principal(%v) failed: %v", origin, err)
	}

	if got == nil {
		t.Fatalf("Expected browspr.principalManager.Principal(%v) to return a valid principal, but got %v", origin, got)
	}
}

func TestHandleAssocAccountWithMissingAccount(t *testing.T) {
	browspr, teardown := setup(t)
	defer teardown()

	account := "mock-account"
	origin := "https://my.webapp.com:443"
	message, err := valueOf(associateAccountMessage{
		Account: account,
		Origin:  origin,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := browspr.HandleAuthAssociateAccountRpc(message); err == nil {
		t.Fatalf("browspr.HandleAuthAssociateAccountRpc(%v) should have failed but did not.")
	}

	// Verify that principalManager creates no principal for the origin
	got, err := browspr.principalManager.Principal(origin)
	if err == nil {
		t.Fatalf("Expected browspr.principalManager.Principal(%v) to fail, but got: %v", origin, got)
	}

	if got != nil {
		t.Fatalf("Expected browspr.principalManager.Principal(%v) not to return a principal, but got %v", origin, got)
	}
}

// Helper to create a *vdl.Value from interface.
// TODO(nlacasse): Todd says there will eventually be a "ValueOf" somewhere
// inside vom/vdl. Remove this once that is available.
func valueOf(in interface{}) (*vdl.Value, error) {
	var out *vdl.Value
	err := valconv.Convert(&out, in)
	return out, err
}
