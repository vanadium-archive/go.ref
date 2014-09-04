// Binary bank is a bank client that interacts with bankd and pbankd.
// Users connect to a bank service and can send commands to it.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"veyron/examples/bank"
	vsecurity "veyron/security"
	idutil "veyron/services/identity/util"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
)

var (
	// TODO(rthellend): Remove the address flag when the config manager is working.
	address               = flag.String("address", "", "the address/endpoint of the bank server")
	serverPattern         = flag.String("server_pattern", string(security.AllPrincipals), "server_pattern is an optional pattern for the expected identity of the fortune server. Example: the pattern \"myorg/fortune\" matches identities with names \"myorg/fortune\" or \"myorg\". If the flag is absent then the default pattern \"...\" matches all identities.")
	accountMountTableName string
)

const bankMountTableName string = "veyron/bank"

// Parses str and returns a nonnegative integer
func getInt(str string, name string) (int64, bool) {
	v, err := strconv.ParseInt(str, 0, 64)
	if err != nil {
		fmt.Printf("given %s %s was not an integer: %s\n", name, str, err)
		return 0, true
	}
	return v, false
}

func main() {
	// Create the Veyron runtime using VEYRON_IDENTITY
	runtime := rt.Init()
	log := runtime.Logger()

	// Construct a new stub that binds to serverEndpoint without
	// using the name service
	var bankServer bank.Bank
	var err error
	if *address != "" {
		bankServer, err = bank.BindBank(naming.JoinAddressName(*address, "//bank"))
	} else {
		bankServer, err = bank.BindBank("veyron/bank")
	}
	if err != nil {
		log.Fatal("error binding to server: ", err)
	}

	ctx, _ := runtime.NewContext().WithTimeout(time.Minute)

	// First, Connect to the bank to retrieve the location you should bind to.
	newIdentity, bankAccountNumber, err := bankServer.Connect(ctx, veyron2.RemoteID(*serverPattern))
	if err != nil {
		log.Fatalf("%s.Connect failed: %s\n", bankMountTableName, err)
	}
	fmt.Printf("Successfully connected to the bank with account: %d\n", bankAccountNumber)

	client := runtime.Client()
	// If you got a new identity, you should update your PrivateID and make a new client with that identity.
	if newIdentity != "" {
		// Decode the new identity
		var newPublicID security.PublicID
		err := idutil.Base64VomDecode(newIdentity, &newPublicID)
		if err != nil {
			log.Fatalf("failed to decode the given public id: %s\n", err)
		}

		// Derive the new PrivateID
		derivedIdentity, err := runtime.Identity().Derive(newPublicID)
		if err != nil {
			log.Fatalf("failed to derive identity: %s\n", err)
		}

		// Update the PrivateID at VEYRON_IDENTITY
		path := os.Getenv("VEYRON_IDENTITY")
		f, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err != nil {
			log.Fatalf("failed to open identity file for writing: %s\n", err)
		}
		err = vsecurity.SaveIdentity(f, derivedIdentity)
		if err != nil {
			log.Fatalf("failed to save identity: %s\n", err)
		}
		err = f.Close()
		if err != nil {
			log.Fatalf("could not close file %q, %s\n", path, err)
		}

		// Make a NewClient and update the other one. Do not close the old client; we need its connection to the Mount Table.
		client, err = runtime.NewClient(veyron2.LocalID(newPublicID))
		if err != nil {
			log.Fatalf("failed to create new client: %s\n", err)
		}
	}

	// Bind to the bank account service
	accountMountTableName = fmt.Sprintf("veyron/bank/%d", bankAccountNumber)
	bindLocation := fmt.Sprintf("//bank/%d", bankAccountNumber)
	var accountServer bank.BankAccount
	if *address != "" { // Connect directly to the server's endpoint
		accountServer, err = bank.BindBankAccount(naming.JoinAddressName(*address, bindLocation), client)
	} else { // Use the Mount Table to connect to the server
		accountServer, err = bank.BindBankAccount(accountMountTableName, client)
	}
	if err != nil {
		log.Fatalf("error binding to server: ", err)
	}

	// Read commands from standard input until the user decides to quit/exit.
	end := false
	for !end {
		fmt.Print("Your command: ")

		// Read input from user
		in := bufio.NewReader(os.Stdin)
		input, _ := in.ReadString('\n')
		commands := strings.Fields(input)

		if len(commands) == 0 {
			fmt.Println("Please enter a command. 'help' displays the list of commands.")
			continue
		}

		switch commands[0] {
		case "bankAccountNumber":
			fmt.Printf("Your bank account number is %d\n", bankAccountNumber)
		case "deposit":
			if len(commands) < 2 {
				fmt.Println("deposit takes an amount")
				break
			}
			amount, e := getInt(commands[1], "amount")
			if e {
				break
			}
			err := accountServer.Deposit(ctx, amount, veyron2.RemoteID(*serverPattern))
			if err != nil {
				fmt.Printf("%s.Deposit failed: %s\n", accountMountTableName, err)
				break
			}
			fmt.Printf("Deposited %d\n", amount)
		case "withdraw":
			if len(commands) < 2 {
				fmt.Println("withdraw takes an amount")
				break
			}
			amount, e := getInt(commands[1], "amount")
			if e {
				break
			}
			err := accountServer.Withdraw(ctx, amount, veyron2.RemoteID(*serverPattern))
			if err != nil {
				fmt.Printf("%s.Withdraw failed: %s\n", accountMountTableName, err)
				break
			}
			fmt.Printf("Withdrew %d\n", amount)
		case "transfer":
			if len(commands) < 3 {
				fmt.Println("transfer takes an account number and an amount")
				break
			}
			accountNumber, e1 := getInt(commands[1], "account number")
			amount, e2 := getInt(commands[2], "amount")
			if e1 || e2 {
				break
			}
			err := accountServer.Transfer(ctx, accountNumber, amount, veyron2.RemoteID(*serverPattern))
			if err != nil {
				fmt.Printf("%s.Transfer failed: %s\n", accountMountTableName, err)
				break
			}
			fmt.Printf("Transferred %d to %d\n", amount, accountNumber)
		case "balance":
			balance, err := accountServer.Balance(ctx, veyron2.RemoteID(*serverPattern))
			if err != nil {
				fmt.Printf("%s.Balance failed: %s\n", accountMountTableName, err)
				break
			}
			fmt.Printf("User has balance %d\n", balance)
		case "help":
			fmt.Println("You can do the following actions:")
			fmt.Println("bankAccountNumber: check your account number")
			fmt.Println("balance: check your account balance")
			fmt.Println("deposit [N>=0]: deposit money into your account")
			fmt.Println("withdraw [N>=0]: withdraw money from your account")
			fmt.Println("transfer [account number] [N>=0]: transfer money to another account")
			fmt.Println("quit: quit the program")
			fmt.Println("exit: also quits the program")
		case "exit", "quit":
			fmt.Println("Quitting...")
			end = true
		default:
			fmt.Printf("Unknown command: %s\n", commands[0])
		}
	}
}
