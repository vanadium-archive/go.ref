// Binary pbankd is a simple implementation of the bank service.
// Binary bank clients can connect to this bank service to manage virtual bank
// accounts. Unlike bankd, pbankd uses the Veyron Store. It can recover user
// data after a crash, but it must always connect to the stored service.
// Unlike bankd, pbankd prevents race conditions with transactions.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"veyron/examples/bank"
	"veyron/examples/bank/schema"
	"veyron/lib/signals"
	"veyron/security/caveat"
	idutil "veyron/services/identity/util"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vlog"
)

// Duration of a bank account blessing, intended to be very long.
const BLESS_DURATION = 24 * 10000 * time.Hour

// Ensure that account numbers are all 6 digits long.
const MIN_ACCOUNT_NUMBER = 100000
const MAX_ACCOUNT_NUMBER = 999999
const SUFFIX_REGEXP = "/[0-9]{6}"

// Where we will store the bank in the store database.
const BANK_ROOT string = "/Bank"

var (
	storeName string
	ACCOUNTS  string
	runtime   veyron2.Runtime
)

func init() {
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	hostname := "unknown"
	if h, err := os.Hostname(); err == nil {
		hostname = h
	}
	dir := "global/vstore/" + hostname + "/" + username

	// Parse the flags (variableName, flagName, defaultValue, description)
	flag.StringVar(&storeName, "store", dir, "Name of the Veyron store")

	// Set the random seed to the current time to increase psuedorandomness.
	rand.Seed(time.Now().Unix())

	// Set the name for the ACCOUNTS 'constant'. TODO(alexfandrianto): Is there a better way to know the field is named "Accounts"?
	ACCOUNTS = reflect.TypeOf(schema.Bank{}).Field(0).Name
}

// The following struct and functions handle construction of the persistent bank.
type pbankd struct {
	// Pointer to the store
	store storage.Store

	// Current Transaction name; empty if there's no transaction
	tname string

	// The bank's private ID (used for blessing)
	ID security.PrivateID
}

// newPbankd creates a new persistent bank structure.
func newPbankd(store storage.Store, identity security.PrivateID) *pbankd {
	b := &pbankd{
		store: store,
		ID:    identity,
	}
	return b
}

// InitializeBank bank details in the store; currently only initializes the root.
func (b *pbankd) initializeBank() {
	if err := b.newTransaction(); err != nil {
		vlog.Fatal(err)
	}
	// NOTE(sadovsky): initializeBankRoot ought to return an error. Currently,
	// some errors (e.g. failed puts) could slip through unnoticed.
	b.initializeBankRoot()
	if err := b.commit(); err != nil {
		vlog.Fatal(err)
	}
}

// initializeBankRoot prepares the bank root as BANK_ROOT if it isn't yet in the Veyron store.
func (b *pbankd) initializeBankRoot() {
	// Create parent directories for the bank root, if necessary
	l := strings.Split(BANK_ROOT, "/")
	fmt.Println(l)
	for i, _ := range l {
		fmt.Println(i)
		prefix := filepath.Join(l[:i]...)
		o := b.store.BindObject(naming.Join(b.tname, prefix))
		if exist, err := o.Exists(runtime.TODOContext()); err != nil {
			vlog.Infof("Error checking existence at %q: %s", prefix, err)
		} else if !exist {
			if _, err := o.Put(runtime.TODOContext(), &schema.Dir{}); err != nil {
				vlog.Infof("Error creating parent %q: %s", prefix, err)
			}
			fmt.Printf("%q was created!\n", prefix)
		} else {
			fmt.Printf("%q was already present in the store.\n", prefix)
		}
	}

	// Add the bank schema to the store at BANK_ROOT, if necessary
	o := b.store.BindObject(naming.Join(b.tname, BANK_ROOT))
	if exist, err := o.Exists(runtime.TODOContext()); err != nil {
		vlog.Infof("Error checking existence at %q: %s", BANK_ROOT, err)
	} else if !exist {
		_, err := o.Put(runtime.TODOContext(), &schema.Bank{})
		if err != nil {
			vlog.Infof("Error creating bank at %q: %s", BANK_ROOT, err)
		}
	}
}

// Register creates an account for a new user with the given bankName.
func (b *pbankd) Connect(context ipc.ServerContext) (string, int64, error) {
	// Check if the RemoteID() has been blessed by the bank
	if num := getBankAccountNumber(context); num != 0 {
		// Look up the user and return their bank account number
		fmt.Println("This client is blessed!")
		fmt.Printf("ID: %d\n", num)
		return "", num, nil
	} else {
		fmt.Println("This client isn't blessed. Let's bless them!")
		// Use the store
		if err := b.newTransaction(); err != nil {
			vlog.Fatal(err)
		}

		// Keep rolling until we get an unseen number
		randID := rand.Int63n(MAX_ACCOUNT_NUMBER-MIN_ACCOUNT_NUMBER) + MIN_ACCOUNT_NUMBER
		for b.isUser(randID) {
			randID = rand.Int63n(MAX_ACCOUNT_NUMBER-MIN_ACCOUNT_NUMBER) + MIN_ACCOUNT_NUMBER
		}
		fmt.Printf("ID: %d\n", randID)

		// Bless the user
		pp := security.PrincipalPattern(context.LocalID().Names()[0])
		pID, err := b.ID.Bless(
			context.RemoteID(),
			fmt.Sprintf("%d", randID),
			BLESS_DURATION,
			[]security.ServiceCaveat{security.UniversalCaveat(caveat.PeerIdentity{pp})},
		)
		if err != nil {
			vlog.Fatal(err)
		}

		// Encode the public ID
		enc, err := idutil.Base64VomEncode(pID)
		if err != nil {
			vlog.Fatal(err)
		}

		// Store the user into the database
		b.registerNewUser(randID)
		err = b.commit()
		if err != nil {
			vlog.Fatal(err)
		}

		// Ensure the new user is a user before returning the blessing and ID
		if b.isUser(randID) {
			return enc, randID, nil
		}
		return "", 0, fmt.Errorf("failed to register user")
	}
}

// Deposit adds the amount given to this account.
func (b *pbankd) Deposit(context ipc.ServerContext, amount int64) error {
	user := getBankAccountNumber(context)
	if user == 0 {
		return fmt.Errorf("couldn't retrieve account number")
	}
	if err := b.newTransaction(); err != nil {
		return err
	}
	if !b.isUser(user) {
		return fmt.Errorf("user isn't registered")
	} else if amount < 0 {
		return fmt.Errorf("deposit amount %d is negative", amount)
	}
	b.changeBalance(user, amount)
	return b.commit()
}

// Withdraw reduces the amount given from this account.
func (b *pbankd) Withdraw(context ipc.ServerContext, amount int64) error {
	user := getBankAccountNumber(context)
	if user == 0 {
		return fmt.Errorf("couldn't retrieve account number")
	}
	if err := b.newTransaction(); err != nil {
		return err
	}
	if !b.isUser(user) {
		return fmt.Errorf("user isn't registered")
	} else if amount < 0 {
		return fmt.Errorf("withdraw amount %d is negative", amount)
	} else if balance := b.checkBalance(user); amount > balance {
		return fmt.Errorf("withdraw amount %d exceeds balance %d", amount, balance)
	}
	b.changeBalance(user, -amount)
	return b.commit()
}

// Transfer moves the amount given to the receiver.
func (b *pbankd) Transfer(context ipc.ServerContext, accountNumber int64, amount int64) error {
	user := getBankAccountNumber(context)
	if user == 0 {
		return fmt.Errorf("couldn't retrieve account number")
	}
	if err := b.newTransaction(); err != nil {
		return err
	}
	if !b.isUser(user) {
		return fmt.Errorf("user isn't registered")
	} else if !b.isUser(accountNumber) {
		return fmt.Errorf("%d isn't registered", accountNumber)
	} else if amount < 0 {
		return fmt.Errorf("transfer amount %d is negative", amount)
	} else if balance := b.checkBalance(user); amount > balance {
		return fmt.Errorf("transfer amount %d exceeds balance %d", amount, balance)
	}
	b.changeBalance(user, -amount)
	b.changeBalance(accountNumber, amount)
	return b.commit()
}

// Balance returns the amount stored by the given user.
// Throws an error if the user is invalid.
func (b *pbankd) Balance(context ipc.ServerContext) (int64, error) {
	user := getBankAccountNumber(context)
	if user == 0 {
		return 0, fmt.Errorf("couldn't retrieve account number")
	}
	if !b.isUser(user) {
		return 0, fmt.Errorf("user isn't registered")
	}
	return b.checkBalance(user), nil
}

/*
 Helper functions for the persistent bank service that deal with store access.
*/

// newTransaction starts a new transaction.
func (b *pbankd) newTransaction() error {
	tid, err := b.store.BindTransactionRoot("").CreateTransaction(runtime.TODOContext())
	if err != nil {
		b.tname = ""
		return err
	}
	b.tname = tid // Transaction is rooted at "", so tname == tid.
	return nil
}

// commit commits the current transaction.
func (b *pbankd) commit() error {
	if b.tname == "" {
		return errors.New("No transaction to commit")
	}
	err := b.store.BindTransaction(b.tname).Commit(runtime.TODOContext())
	b.tname = ""
	if err != nil {
		return fmt.Errorf("Failed to commit transaction: %s", err)
	}
	return nil
}

// isUser helps determine if the given user is part of the system or not.
func (b *pbankd) isUser(accountNumber int64) bool {
	// If this is a user, their location in the store should exist.
	prefix := filepath.Join(BANK_ROOT, ACCOUNTS, fmt.Sprintf("%d", accountNumber))
	o := b.store.BindObject(naming.Join(b.tname, prefix))
	exist, err := o.Exists(runtime.TODOContext())
	if err != nil {
		vlog.Infof("Error checking existence at %s: %s", prefix, err)
		return false
	}
	return exist
}

// Obtains the bank account number of the user. Returns 0 if it could not be found.
func getBankAccountNumber(context ipc.ServerContext) int64 {
	bankName := context.LocalID().Names()[0]

	// Untrusted clients have no names.
	if len(context.RemoteID().Names()) == 0 {
		return 0
	}

	// Otherwise, extract the account number from the name.
	name := context.RemoteID().Names()[0]
	if match, err := regexp.MatchString(bankName+SUFFIX_REGEXP, name); err != nil {
		vlog.Infof("MatchString error: %s", err)
		return 0
	} else if !match {
		vlog.Infof("No matching name found")
		return 0
	}
	var v int64
	_, err := fmt.Sscanf(name, bankName+"/%d", &v)
	if err != nil {
		vlog.Infof("Failure to parse ID from %s: %s", name, err)
		return 0
	}
	return v
}

// registerNewUser adds the user to the system under the specified name and returns success.
func (b *pbankd) registerNewUser(user int64) {
	// Create the user's account
	prefix := filepath.Join(BANK_ROOT, ACCOUNTS, fmt.Sprintf("%d", user))
	o := b.store.BindObject(naming.Join(b.tname, prefix))
	if _, err := o.Put(runtime.TODOContext(), int64(0)); err != nil {
		vlog.Infof("Error creating %s: %s", prefix, err)
	}
}

// checkBalance gets the user's balance from the store
func (b *pbankd) checkBalance(user int64) int64 {
	prefix := filepath.Join(BANK_ROOT, ACCOUNTS, fmt.Sprintf("%d", user))
	o := b.store.BindObject(naming.Join(b.tname, prefix))
	e, err := o.Get(runtime.TODOContext())
	if err != nil {
		vlog.Infof("Error getting %s: %s", prefix, err)
	}
	value, _ := e.Value.(int64)
	return value
}

// changeBalance modifies the user's balance in the store
func (b *pbankd) changeBalance(user int64, amount int64) {
	prefix := filepath.Join(BANK_ROOT, ACCOUNTS, fmt.Sprintf("%d", user))
	o := b.store.BindObject(naming.Join(b.tname, prefix))
	e, err := o.Get(runtime.TODOContext())
	if err != nil {
		vlog.Infof("Error getting %s: %s", prefix, err)
	}
	if _, err := o.Put(runtime.TODOContext(), e.Value.(int64)+amount); err != nil {
		vlog.Infof("Error changing %s: %s", prefix, err)
	}
}

// This custom bank dispatcher has two interfaces with distinct authorizers.
func newBankDispatcher(bankServer interface{}, bankAccountServer interface{}, authBank security.Authorizer, authBankAccount security.Authorizer) ipc.Dispatcher {
	return BankDispatcher{ipc.ReflectInvoker(bankServer), ipc.ReflectInvoker(bankAccountServer), authBank, authBankAccount}
}

type BankDispatcher struct {
	invokerBank        ipc.Invoker
	invokerBankAccount ipc.Invoker
	authBank           security.Authorizer
	authBankAccount    security.Authorizer
}

func (d BankDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	fmt.Println("Dispatcher Lookup Suffix:", suffix)
	if suffix != "" {
		return d.invokerBankAccount, d.authBankAccount, nil
	}
	return d.invokerBank, d.authBank, nil
}

// The custom account authorizer checks if the RemoteID matches a regexp pattern and allows only Reads and Writes.
type AccountAuthorizer string

func (aa AccountAuthorizer) Authorize(ctx security.Context) error {
	name := ctx.RemoteID().Names()[0]
	match, err := regexp.MatchString(string(aa), name)
	fmt.Printf("Authorizing for Account %s %t\n", name, match)
	if err != nil {
		return err
	}
	if match {
		if ctx.Label() != security.ReadLabel && ctx.Label() != security.WriteLabel {
			return errors.New("unauthorized; may only read or write")
		}
		return nil
	}
	return errors.New("unauthorized to access account")
}

func main() {
	// Create a new server instance.
	runtime = rt.Init()
	s, err := runtime.NewServer()
	if err != nil {
		vlog.Fatal("failure creating server: ", err)
	}

	// Connect to the Veyron Store
	vlog.Infof("Binding to store on %s", storeName)
	st, err := vstore.New(storeName)
	if err != nil {
		vlog.Fatalf("Can't connect to store: %s: %s", storeName, err)
	}

	// Create the bank server and bank account server stubs, using the store connection
	pbankd := newPbankd(st, runtime.Identity())
	pbankd.initializeBank()
	bankServer := bank.NewServerBank(pbankd)
	bankAccountServer := bank.NewServerBankAccount(pbankd)

	// Setup bank and account authorizers.
	bankAuth := security.NewACLAuthorizer(security.ACL{security.AllPrincipals: security.LabelSet(security.ReadLabel | security.WriteLabel)})
	bankAccountAuth := AccountAuthorizer(runtime.Identity().PublicID().Names()[0] + SUFFIX_REGEXP)

	dispatcher := newBankDispatcher(bankServer, bankAccountServer, bankAuth, bankAccountAuth)

	// Create an endpoint and begin listening.
	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		fmt.Printf("Listening at: %v\n", endpoint)
	} else {
		vlog.Fatal("error listening to service: ", err)
	}

	// Publish the service in the mount table.
	mountName := "veyron/bank"
	fmt.Printf("Mounting bank on %s, endpoint /%s\n", mountName, endpoint)
	if err := s.Serve(mountName, dispatcher); err != nil {
		vlog.Fatal("s.Serve() failed: ", err)
	}

	// Wait forever.
	<-signals.ShutdownOnSignals()
}
