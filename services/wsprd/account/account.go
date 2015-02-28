// The account package contains logic for creating accounts and associating them with origins.
package account

import (
	"fmt"
	"strings"
	"time"

	"v.io/core/veyron/services/wsprd/principal"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
)

type BlesserService interface {
	BlessUsingAccessToken(ctx *context.T, token string, opts ...ipc.CallOpt) (blessingObj security.WireBlessings, account string, err error)
}

type bs struct {
	name string
}

func (s *bs) BlessUsingAccessToken(ctx *context.T, token string, opts ...ipc.CallOpt) (blessingObj security.WireBlessings, account string, err error) {
	client := v23.GetClient(ctx)
	var call ipc.ClientCall
	if call, err = client.StartCall(ctx, s.name, "BlessUsingAccessToken", []interface{}{token}, opts...); err != nil {
		return
	}
	var email string
	if err := call.Finish(&blessingObj, &email); err != nil {
		return security.WireBlessings{}, "", err
	}
	serverBlessings, _ := call.RemoteBlessings()
	return blessingObj, accountName(serverBlessings, email), nil
}

func accountName(serverBlessings []string, email string) string {
	return strings.Join(serverBlessings, "#") + security.ChainSeparator + email
}

type AccountManager struct {
	ctx              *context.T
	blesser          BlesserService
	principalManager *principal.PrincipalManager
	accounts         []string
}

func NewAccountManager(identitydEndpoint string, principalManager *principal.PrincipalManager) *AccountManager {
	return &AccountManager{
		blesser:          &bs{name: identitydEndpoint},
		principalManager: principalManager,
	}
}

func (am *AccountManager) CreateAccount(ctx *context.T, accessToken string) (string, error) {
	// Get a blessing for the access token from blessing server.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
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

	am.accounts = append(am.accounts, account)

	return account, nil
}

func (am *AccountManager) GetAccounts() []string {
	return am.accounts
}

func (am *AccountManager) AssociateAccount(origin, account string, cavs []Caveat) error {
	caveats, expirations, err := constructCaveats(cavs)
	if err != nil {
		return fmt.Errorf("failed to construct caveats: %v", err)
	}
	// Store the origin.
	if err := am.principalManager.AddOrigin(origin, account, caveats, expirations); err != nil {
		return fmt.Errorf("failed to associate account: %v", err)
	}
	vlog.VI(1).Infof("Associated origin %v with account %v and cavs %v", origin, account, caveats)
	return nil
}

func (am *AccountManager) LookupPrincipal(origin string) (security.Principal, error) {
	return am.principalManager.Principal(origin)
}

func (am *AccountManager) OriginHasAccount(origin string) bool {
	return am.principalManager.OriginHasAccount(origin)
}

func (am *AccountManager) PrincipalManager() *principal.PrincipalManager {
	return am.principalManager
}

// TODO(bprosnitz) Refactor WSPR to remove this.
func (am *AccountManager) SetMockBlesser(blesser BlesserService) {
	am.blesser = blesser
}

func constructCaveats(cavs []Caveat) ([]security.Caveat, []time.Time, error) {
	var caveats []security.Caveat
	var expirations []time.Time

	for _, cav := range cavs {
		var (
			caveat     security.Caveat
			expiration time.Time
			err        error
		)
		switch cav.Type {
		case "ExpiryCaveat":
			caveat, expiration, err = createExpiryCaveat(cav.Args)
			expirations = append(expirations, expiration)
		case "MethodCaveat":
			caveat, err = createMethodCaveat(cav.Args)
		default:
			return nil, nil, fmt.Errorf("caveat %v does not exist", cav.Type)
		}
		if err != nil {
			return nil, nil, err
		}
		caveats = append(caveats, caveat)
	}
	return caveats, expirations, nil
}

func createExpiryCaveat(arg string) (security.Caveat, time.Time, error) {
	var zeroTime time.Time
	dur, err := time.ParseDuration(arg)
	if err != nil {
		return security.Caveat{}, zeroTime, fmt.Errorf("time.parseDuration(%v) failed: %v", arg, err)
	}
	expirationTime := time.Now().Add(dur)
	cav, err := security.ExpiryCaveat(expirationTime)
	if err != nil {
		return security.Caveat{}, zeroTime, fmt.Errorf("security.ExpiryCaveat(%v) failed: %v", expirationTime, err)
	}
	return cav, expirationTime, nil
}

func createMethodCaveat(a string) (security.Caveat, error) {
	args := strings.Split(a, ",")
	if len(args) == 0 {
		return security.Caveat{}, fmt.Errorf("must pass at least one method")
	}
	return security.MethodCaveat(args[0], args[1:]...)
}
