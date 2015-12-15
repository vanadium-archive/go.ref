// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package browspr

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdl"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/wspr/internal/principal"
	"v.io/x/ref/test"
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

func (m *mockBlesserService) BlessUsingAccessToken(c *context.T, accessToken string, co ...rpc.CallOpt) (security.Blessings, string, error) {
	m.count++
	name := fmt.Sprintf("%s%s%d", topLevelName, security.ChainSeparator, m.count)
	blessing, err := m.p.BlessSelf(name)
	if err != nil {
		return blessing, "", err
	}
	return blessing, name, nil
}

func setup(t *testing.T) (*Browspr, func()) {
	ctx, shutdown := test.V23Init()

	spec := v23.GetListenSpec(ctx)
	spec.Proxy = "/mock/proxy"
	mockPostMessage := func(_ int32, _, _ string) {}
	browspr := NewBrowspr(ctx, mockPostMessage, &spec, "/mock:1234/identd", nil, principal.NewInMemorySerializer())
	principal := v23.GetPrincipal(browspr.ctx)
	browspr.accountManager.SetMockBlesser(newMockBlesserService(principal))

	return browspr, func() {
		browspr.Shutdown()
		shutdown()
	}
}

func TestHandleCreateAccount(t *testing.T) {
	browspr, teardown := setup(t)
	defer teardown()

	// Verify that HandleAuthGetAccountsRpc returns empty.
	nilValue := vdl.ValueOf(GetAccountsMessage{})
	a, err := browspr.HandleAuthGetAccountsRpc(nilValue)
	if err != nil {
		t.Fatalf("browspr.HandleAuthGetAccountsRpc(%v) failed: %v", nilValue, err)
	}
	if a.Len() > 0 {
		t.Fatalf("Expected accounts to be empty array but got %v", a)
	}

	// Add one account.
	message1 := vdl.ValueOf(CreateAccountMessage{
		Token: "mock-access-token-1",
	})
	account1, err := browspr.HandleAuthCreateAccountRpc(message1)
	if err != nil {
		t.Fatalf("browspr.HandleAuthCreateAccountRpc(%v) failed: %v", message1, err)
	}

	// Verify that principalManager has the new account
	if b, err := browspr.principalManager.BlessingsForAccount(account1.RawString()); err != nil || b.IsZero() {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account1, b, err)
	}

	// Verify that HandleAuthGetAccountsRpc returns the new account.
	gotAccounts1, err := browspr.HandleAuthGetAccountsRpc(nilValue)
	if err != nil {
		t.Fatalf("browspr.HandleAuthGetAccountsRpc(%v) failed: %v", nilValue, err)
	}
	if want := vdl.ValueOf([]string{account1.RawString()}); !vdl.EqualValue(want, gotAccounts1) {
		t.Fatalf("Expected account to be %v but got empty but got %v", want, gotAccounts1)
	}

	// Add another account
	message2 := vdl.ValueOf(CreateAccountMessage{
		Token: "mock-access-token-2",
	})
	account2, err := browspr.HandleAuthCreateAccountRpc(message2)
	if err != nil {
		t.Fatalf("browspr.HandleAuthCreateAccountsRpc(%v) failed: %v", message2, err)
	}

	// Verify that HandleAuthGetAccountsRpc returns the new account.
	gotAccounts2, err := browspr.HandleAuthGetAccountsRpc(nilValue)
	if err != nil {
		t.Fatalf("browspr.HandleAuthGetAccountsRpc(%v) failed: %v", nilValue, err)
	}
	var got []string
	if err := vdl.Convert(&got, gotAccounts2); err != nil {
		t.Fatalf("vdl.Convert failed: %v", err)
	}
	sort.Strings(got)
	if want := []string{account1.RawString(), account2.RawString()}; !reflect.DeepEqual(want, got) {
		t.Fatalf("Expected account to be %v but got %v", want, got)
	}

	// Verify that principalManager has both accounts
	if b, err := browspr.principalManager.BlessingsForAccount(account1.RawString()); err != nil || b.IsZero() {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account1, b, err)
	}
	if b, err := browspr.principalManager.BlessingsForAccount(account2.RawString()); err != nil || b.IsZero() {
		t.Fatalf("Failed to get Blessings for account %v: got %v, %v", account2, b, err)
	}
}

func TestHandleAssocAccount(t *testing.T) {
	browspr, teardown := setup(t)
	defer teardown()

	// First create an account.
	account := "mock-account"
	principal := v23.GetPrincipal(browspr.ctx)
	blessing, err := principal.BlessSelf(account)
	if err != nil {
		t.Fatalf("browspr.rt.Principal.BlessSelf(%v) failed: %v", account, err)
	}
	if err := browspr.principalManager.AddAccount(account, blessing); err != nil {
		t.Fatalf("browspr.principalManager.AddAccount(%v, %v) failed; %v", account, blessing, err)
	}

	origin := "https://my.webapp.com:443"

	// Verify that HandleAuthOriginHasAccountRpc returns false
	hasAccountMessage := vdl.ValueOf(OriginHasAccountMessage{
		Origin: origin,
	})
	hasAccount, err := browspr.HandleAuthOriginHasAccountRpc(hasAccountMessage)
	if err != nil {
		t.Fatal(err)
	}
	if hasAccount.Bool() {
		t.Fatalf("Expected browspr.HandleAuthOriginHasAccountRpc(%v) to be false but was true", hasAccountMessage)
	}

	assocAccountMessage := vdl.ValueOf(AssociateAccountMessage{
		Account: account,
		Origin:  origin,
	})

	if _, err := browspr.HandleAuthAssociateAccountRpc(assocAccountMessage); err != nil {
		t.Fatalf("browspr.HandleAuthAssociateAccountRpc(%v) failed: %v", assocAccountMessage, err)
	}

	// Verify that HandleAuthOriginHasAccountRpc returns true
	hasAccount, err = browspr.HandleAuthOriginHasAccountRpc(hasAccountMessage)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAccount.Bool() {
		t.Fatalf("Expected browspr.HandleAuthOriginHasAccountRpc(%v) to be true but was false", hasAccountMessage)
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
	message := vdl.ValueOf(AssociateAccountMessage{
		Account: account,
		Origin:  origin,
	})

	if _, err := browspr.HandleAuthAssociateAccountRpc(message); err == nil {
		t.Fatalf("browspr.HandleAuthAssociateAccountRpc(%v) should have failed but did not.", message)
	}

	// Verify that principalManager creates no principal for the origin
	got, err := browspr.principalManager.Principal(origin)
	if err == nil {
		t.Fatalf("Expected browspr.principalManager.Principal(%v) to fail, but got: %v", origin, got)
	}

	if got != nil {
		t.Fatalf("Expected browspr.principalManager.Principal(%v) not to return a principal, but got %v", origin, got)
	}

	// Verify that HandleAuthOriginHasAccountRpc returns false
	hasAccountMessage := vdl.ValueOf(OriginHasAccountMessage{
		Origin: origin,
	})
	hasAccount, err := browspr.HandleAuthOriginHasAccountRpc(hasAccountMessage)
	if err != nil {
		t.Fatal(err)
	}
	if hasAccount.Bool() {
		t.Fatalf("Expected browspr.HandleAuthOriginHasAccountRpc(%v) to be false but was true", hasAccountMessage)
	}
}
