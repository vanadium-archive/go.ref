// The account package contains logic for creating accounts and associating them with origins.
package account

import (
	"fmt"
	"strings"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/wspr/veyron/services/wsprd/principal"
)

type BlesserService interface {
	BlessUsingAccessToken(ctx context.T, token string, opts ...ipc.CallOpt) (blessingObj security.WireBlessings, account string, err error)
}

type bs struct {
	client ipc.Client
	name   string
}

func (s *bs) BlessUsingAccessToken(ctx context.T, token string, opts ...ipc.CallOpt) (blessingObj security.WireBlessings, account string, err error) {
	var call ipc.Call
	if call, err = s.client.StartCall(ctx, s.name, "BlessUsingAccessToken", []interface{}{token}, opts...); err != nil {
		return
	}
	var email string
	if ierr := call.Finish(&blessingObj, &email, &err); ierr != nil {
		err = ierr
	}
	serverBlessings, _ := call.RemoteBlessings()
	return blessingObj, accountName(serverBlessings, email), nil
}

func accountName(serverBlessings []string, email string) string {
	return strings.Join(serverBlessings, "#") + security.ChainSeparator + email
}

type AccountManager struct {
	rt               veyron2.Runtime
	blesser          BlesserService
	principalManager *principal.PrincipalManager
}

func NewAccountManager(rt veyron2.Runtime, identitydEndpoint string, principalManager *principal.PrincipalManager) *AccountManager {
	return &AccountManager{
		rt:               rt,
		blesser:          &bs{client: rt.Client(), name: identitydEndpoint},
		principalManager: principalManager,
	}
}

func (am *AccountManager) CreateAccount(accessToken string) (string, error) {
	// Get a blessing for the access token from blessing server.
	ctx, cancel := am.rt.NewContext().WithTimeout(time.Minute)
	defer cancel()
	blessings, account, err := am.blesser.BlessUsingAccessToken(ctx, accessToken)
	if err != nil {
		return "", fmt.Errorf("Error getting blessing for access token: %v", err)
	}

	accountBlessings, err := security.NewBlessings(blessings)
	if err != nil {
		return "", fmt.Errorf("Error creating blessings from wire data: %v", err)
	}

	// Add accountBlessings to principalManager under the provided
	// account.
	if err := am.principalManager.AddAccount(account, accountBlessings); err != nil {
		return "", fmt.Errorf("Error adding account: %v", err)
	}

	return account, nil
}

func (am *AccountManager) AssociateAccount(origin, account string) error {
	// Store the origin.
	// TODO(nlacasse, bjornick): determine what the caveats should be.
	if err := am.principalManager.AddOrigin(origin, account, nil); err != nil {
		return fmt.Errorf("Error associating account: %v", err)
	}
	return nil
}

func (am *AccountManager) LookupPrincipal(origin string) (security.Principal, error) {
	return am.principalManager.Principal(origin)
}

func (am *AccountManager) PrincipalManager() *principal.PrincipalManager {
	return am.principalManager
}

// TODO(bprosnitz) Refactor WSPR to remove this.
func (am *AccountManager) SetMockBlesser(blesser BlesserService) {
	am.blesser = blesser
}
