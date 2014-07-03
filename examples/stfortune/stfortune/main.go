// Binary stfortune is a simple client of Veyron Store.  See
// http://go/veyron:codelab-store for a thorough explanation.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"veyron/examples/stfortune/schema"

	"veyron2/context"
	"veyron2/naming"
	"veyron2/rt"
	istore "veyron2/services/store"
	iwatch "veyron2/services/watch"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/storage/vstore/primitives"
	"veyron2/vom"
)

func fortunePath(name string) string {
	return naming.Join(naming.Join(appPath, "fortunes"), name)
}

func userPath(name string) string {
	return naming.Join(naming.Join(appPath, "usernames"), name)
}

// Hashes a string.
func getMD5Hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

// waitForStore waits for the local store to be ready by checking if
// the schema information is synchronized.
func waitForStore(store storage.Store) {
	ctx := rt.R().NewContext()

	// Register *store.Entry for WatchGlob.
	// TODO(tilaks): store.Entry is declared in vdl, vom should register the
	// pointer automatically.
	vom.Register(&istore.Entry{})

	fmt.Printf("Waiting for Store to be initialized with fortune schema...\n")
	// List of paths to check in store.
	paths := []string{appPath, fortunePath(""), userPath("")}
	for _, path := range paths {
		req := iwatch.GlobRequest{Pattern: ""}
		stream, err := store.Bind(path).WatchGlob(ctx, req)
		if err != nil {
			log.Fatalf("WatchGlob %s failed: %v", path, err)
		}
		if _, err := stream.Recv(); err != nil {
			log.Fatalf("Recv failed: %v", err)
		}
		stream.Cancel()
	}

	fmt.Printf("Store is ready\n")
	return
}

// pickFortune finds all available fortunes under the input path and
// chooses one randomly.
func pickFortune(store storage.Store, ctx context.T, path string) (string, error) {
	tr := primitives.NewTransaction(ctx)
	defer tr.Abort(ctx)

	results, err := store.Bind(path).GlobT(ctx, tr, "*")
	if err != nil {
		return "", err
	}
	var names []string
	for {
		name, err := results.Recv()
		if err != nil {
			break
		}
		names = append(names, name)
	}
	results.Finish()
	if names == nil || len(names) < 1 {
		return "", nil
	}

	// Get a random fortune using the glob results.
	random := rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	p := fortunePath(names[random.Intn(len(names))])
	f, err := store.Bind(p).Get(ctx, tr)
	if err != nil {
		return "", err
	}
	fortune, ok := f.Value.(schema.FortuneData)
	if !ok {
		return "", fmt.Errorf("found type %T, expected schema.FortuneData", f.Value)
	}
	return fortune.Fortune, nil
}

// getFortune returns a random fortune corresponding to a UserName if
// specified. If not, it picks a random fortune.
func getFortune(store storage.Store, userName string) (string, error) {
	ctx := rt.R().NewContext()

	var p string
	if userName != "" {
		// Look for a random fortune belonging to UserName.
		p = userPath(userName)
	} else {
		// Look for a random fortune.
		p = fortunePath("")
	}

	return pickFortune(store, ctx, p)
}

// addFortune adds a new fortune to the store and links it to the specified
// UserName. In this process, if the UserName doesn't exist, a new
// user is created.
func addFortune(store storage.Store, fortune string, userName string) error {
	ctx := rt.R().NewContext()
	tr := primitives.NewTransaction(ctx)
	committed := false
	defer func() {
		if !committed {
			tr.Abort(ctx)
		}
	}()

	// Check if this fortune already exists. If yes, return.
	hash := getMD5Hash(naming.Join(fortune, userName))
	exists, err := store.Bind(fortunePath(hash)).Exists(ctx, tr)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Check if the UserName exists. If yes, get its OID. If not, create a new user.
	o := store.Bind(userPath(userName))
	exists, err = o.Exists(ctx, tr)
	if err != nil {
		return err
	}
	var userid storage.ID
	if !exists {
		u := schema.User{Name: userName}
		stat, err := o.Put(ctx, tr, u)
		if err != nil {
			return err
		}
		userid = stat.ID
	} else {
		u, err := o.Get(ctx, tr)
		if err != nil {
			return err
		}
		userid = u.Stat.ID
	}

	// Create a new fortune entry.
	f := schema.FortuneData{Fortune: fortune, UserName: userid}
	s, err := store.Bind(fortunePath(hash)).Put(ctx, tr, f)
	if err != nil {
		return err
	}

	// Link the new fortune to UserName.
	p := userPath(naming.Join(userName, hash))
	if _, err = store.Bind(p).Put(ctx, tr, s.ID); err != nil {
		return err
	}

	// Commit all changes.
	//
	// NOTE: A commit can sometimes fail due to store's optimistic
	// locking. When the error for this scenario is
	// exposed via the Commit API, one could retry the
	// transaction.
	if err := tr.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

var (
	appPath      = "/apps/stfortune"
	storeAddress = flag.String("store", "", "the address/endpoint of the Veyron Store")
	newFortune   = flag.String("new_fortune", "", "an optional, new fortune to add to the server's set")
	user         = flag.String("user_name", "", "an optional username of the fortune creator to get/add to the server's set")
)

func main() {
	rt.Init()
	if *storeAddress == "" {
		log.Fatal("--store needs to be specified")
	}

	// Create a handle to the backend store.
	store, err := vstore.New(*storeAddress)
	if err != nil {
		log.Fatalf("Can't connect to store: %s: %v", *storeAddress, err)
	}

	// Wait for the store to be ready before proceeding.
	waitForStore(store)

	// Get a fortune from the store.
	fortune, err := getFortune(store, *user)
	if err != nil {
		log.Fatal("error getting fortune: ", err)
	}
	fmt.Println("Fortune: ", fortune)

	// If the user specified --new_fortune, add it to the store’s set of fortunes.
	if *newFortune != "" {
		if *user == "" {
			*user = "anonymous"
		}
		*user = strings.ToLower(*user)
		if err := addFortune(store, *newFortune, *user); err != nil {
			log.Fatal("error adding fortune: ", err)
		}
	}
}
