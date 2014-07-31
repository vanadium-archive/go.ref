// Binary stfortune is a simple client of Veyron Store.  See
// http://go/veyron:codelab-store for a thorough explanation.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"
	"veyron2/query"

	"veyron/examples/stfortune/schema"

	"veyron2/context"
	"veyron2/naming"
	"veyron2/rt"
	istore "veyron2/services/store"
	iwatch "veyron2/services/watch"
	"veyron2/storage"
	"veyron2/storage/vstore"
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
		stream, err := store.BindObject(path).WatchGlob(ctx, req)
		if err != nil {
			log.Fatalf("WatchGlob %s failed: %v", path, err)
		}
		if !stream.RecvStream().Advance() {
			log.Fatalf("waitForStore, path: %s, Advance failed: %v", path, stream.RecvStream().Err())
		}
		stream.Cancel()
	}

	fmt.Printf("Store is ready\n")
	return
}

// runAsWatcher monitors updates to the fortunes in the store and
// prints out that information.  It does not return.
func runAsWatcher(store storage.Store, user string) {
	// TODO(tilaks): remove this when the store.Entry is auto-registered by VOM.
	vom.Register(&istore.Entry{})
	ctx := rt.R().NewContext()

	// Monitor all new fortunes or only those of a specific user.
	var path string
	if user == "" {
		path = fortunePath("")
	} else {
		path = userPath(user)
	}
	fmt.Printf("Running as a Watcher monitoring new fortunes under %s...\n", path)

	req := iwatch.GlobRequest{Pattern: "*"}
	stream, err := store.BindObject(path).WatchGlob(ctx, req)
	if err != nil {
		log.Fatalf("watcher WatchGlob %s failed: %v", path, err)
	}

	rStream := stream.RecvStream()
	for rStream.Advance() {
		batch := rStream.Value()

		for _, change := range batch.Changes {
			entry, ok := change.Value.(*storage.Entry)
			if !ok {
				log.Printf("watcher change Value not a storage Entry: %#v", change.Value)
				continue
			}

			fortune, ok := entry.Value.(schema.FortuneData)
			if !ok {
				log.Printf("watcher data not a FortuneData Entry: %#v", entry.Value)
				continue
			}

			fmt.Printf("watcher: new fortune: %s\n", fortune.Fortune)
		}
	}
	err = rStream.Err()
	if err == nil {
		err = io.EOF
	}
	log.Fatalf("watcher Advance failed: %v", err)
}

// pickFortuneGlob uses Glob to find all available fortunes under the input
// path and then it chooses one randomly.
func pickFortuneGlob(store storage.Store, ctx context.T, path string) (string, error) {
	// Transaction is rooted at "", so tname == tid.
	tname, err := store.BindTransactionRoot("").CreateTransaction(ctx)
	if err != nil {
		return "", err
	}

	// This transaction is read-only, so we always abort it at the end.
	defer store.BindTransaction(tname).Abort(ctx)

	trPath := func(path string) string {
		return naming.Join(tname, path)
	}

	results := store.BindObject(trPath(path)).Glob(ctx, "*")
	var names []string
	rStream := results.RecvStream()
	for rStream.Advance() {
		names = append(names, rStream.Value())
	}
	if err := results.Err(); err != nil || len(names) == 0 {
		return "", err
	}

	// Get a random fortune using the glob results.
	random := rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
	p := fortunePath(names[random.Intn(len(names))])
	f, err := store.BindObject(trPath(p)).Get(ctx)
	if err != nil {
		return "", err
	}
	fortune, ok := f.Value.(schema.FortuneData)
	if !ok {
		return "", fmt.Errorf("found type %T, expected schema.FortuneData", f.Value)
	}
	return fortune.Fortune, nil
}

// pickFortuneQuery uses a query to find all available fortunes under the input
// path and choose one randomly.
func pickFortuneQuery(store storage.Store, ctx context.T, path string) (string, error) {
	results := store.BindObject(path).Query(ctx,
		query.Query{
			"* |" + // Inspect all children of path.
				"type FortuneData |" + // Include only objects of type FortuneData.
				"{Fortune: Fortune} |" + // Create a new struct containing only the Fortune field.
				"sample(1)", // Randomly select one.
		})
	for results.Advance() {
		f := results.Value().Fields()["Fortune"]
		fortune, ok := f.(string)
		if !ok {
			return "", fmt.Errorf("unexpected type for fortune, got %T, expected string", f)
		}
		results.Cancel()
		return fortune, nil
	}
	if results.Err() != nil {
		return "", results.Err()
	}
	return "", nil // No fortunes found.
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

	switch *pickMethod {
	case "glob":
		return pickFortuneGlob(store, ctx, p)
	case "query":
		return pickFortuneQuery(store, ctx, p)
	default:
		return "", fmt.Errorf("unsupported value for --pick_method.  use 'glob' or 'query'")
	}
}

// addFortune adds a new fortune to the store and links it to the specified
// UserName. In this process, if the UserName doesn't exist, a new
// user is created.
func addFortune(store storage.Store, fortune string, userName string) error {
	ctx := rt.R().NewContext()

	// Transaction is rooted at "", so tname == tid.
	tname, err := store.BindTransactionRoot("").CreateTransaction(ctx)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			store.BindTransaction(tname).Abort(ctx)
		}
	}()

	trPath := func(path string) string {
		return naming.Join(tname, path)
	}

	// Check if this fortune already exists. If yes, return.
	hash := getMD5Hash(naming.Join(fortune, userName))
	exists, err := store.BindObject(trPath(fortunePath(hash))).Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Check if the UserName exists. If yes, get its OID. If not, create a new user.
	o := store.BindObject(trPath(userPath(userName)))
	exists, err = o.Exists(ctx)
	if err != nil {
		return err
	}
	var userid storage.ID
	if !exists {
		u := schema.User{Name: userName}
		stat, err := o.Put(ctx, u)
		if err != nil {
			return err
		}
		userid = stat.ID
	} else {
		u, err := o.Get(ctx)
		if err != nil {
			return err
		}
		userid = u.Stat.ID
	}

	// Create a new fortune entry.
	f := schema.FortuneData{Fortune: fortune, UserName: userid}
	s, err := store.BindObject(trPath(fortunePath(hash))).Put(ctx, f)
	if err != nil {
		return err
	}

	// Link the new fortune to UserName.
	p := userPath(naming.Join(userName, hash))
	if _, err = store.BindObject(trPath(p)).Put(ctx, s.ID); err != nil {
		return err
	}

	// Commit all changes.
	//
	// NOTE: A commit can sometimes fail due to store's optimistic
	// locking. When the error for this scenario is
	// exposed via the Commit API, one could retry the
	// transaction.
	if err := store.BindTransaction(tname).Commit(ctx); err != nil {
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
	watch        = flag.Bool("watch", false, "run as a watcher reporting new fortunes")
	pickMethod   = flag.String("pick_method", "glob", "use 'glob' or 'query' to randomly select a fortune")
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

	// Run as a watcher if --watch is set.
	if *watch {
		runAsWatcher(store, *user)
		os.Exit(0)
	}
}
