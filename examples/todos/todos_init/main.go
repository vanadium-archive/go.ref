// todos_init is a tool to initialize the store with an initial database. This
// is really for demo purposes; in a real database, the contents would be
// persistant.
//
// The data is loaded from a JSON file, todos_init/data.json.
//
// Since JSON doesn't support all of the store types, there is a translation
// phase, where the contents are loaded into a string form, then converted to
// the todos/schema schema.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"veyron/examples/todos/schema"
	"veyron2"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/storage/vstore/primitives"
	"veyron2/vlog"
)

var (
	storeName string
	dataPath  = flag.String("data-path", "data.json", "Path to data JSON file")
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
	// TODO(sadovsky): Change this to be the correct veyron2 path.
	dir := "global/vstore/" + hostname + "/" + username
	flag.StringVar(&storeName, "store", dir, "Name of the Veyron store")
}

// List is the JSON representation for schema.List.
// Note, we use this representation for parity with the Meteor example.
// https://www.meteor.com/examples/todos
type List struct {
	Name string
	// Each element corresponds to a schema.Item; each item is represented as a
	// list where the first element is the item name and the remaining elements
	// are tags.
	Contents [][]string
}

// state is the initial store state.
type state struct {
	store       storage.Store
	transaction storage.Transaction
}

// newState returns a fresh state.
func newState(st storage.Store) *state {
	return &state{store: st}
}

// put adds a value to the store, creating the path to the value if it doesn't
// already exist.
func (st *state) put(path string, v interface{}) {
	vlog.Infof("Storing %q = %+v", path, v)
	st.makeParentDirs(path)
	if _, err := st.store.Bind(path).Put(st.transaction, v); err != nil {
		vlog.Errorf("put failed: %s: %s", path, err)
		return
	}
}

// makeParentDirs creates the directories in a path if they do not already
// exist.
func (st *state) makeParentDirs(path string) {
	l := strings.Split(path, "/")
	for i, _ := range l {
		prefix := filepath.Join(l[:i]...)
		o := st.store.Bind(prefix)
		if _, err := o.Get(st.transaction); err != nil {
			if _, err := o.Put(st.transaction, &schema.Dir{}); err != nil {
				vlog.Errorf("Error creating parent %q: %s", prefix, err)
			}
		}
	}
}

// newTransaction starts a new transaction.
func (st *state) newTransaction() {
	st.transaction = primitives.NewTransaction()
}

// commit commits the current transaction.
func (st *state) commit() {
	if err := st.transaction.Commit(); err != nil {
		vlog.Errorf("Failed to commit transaction: %s", err)
	}
	st.transaction = nil
}

// storeList saves a schema.List to the store with name /lists/<Name>, and also
// saves the list's child items.
func (st *state) storeList(l *List) {
	x := &schema.List{
		Name: l.Name,
	}
	path := "/lists/" + x.Name
	st.put(path, x)

	// Store this list's child items.
	// TODO(sadovsky): Ensure that order is preserved, and is reflected in the UI.
	for i, v := range l.Contents {
		st.storeItem(path, i, v[0], v[1:])
	}
}

// storeItem saves a schema.Item to the store with name /<listPath>/Items/<id>.
// Note that <id> is defined by storeList to be the position of the item in its
// parent list.
func (st *state) storeItem(listPath string, id int, text string, tags []string) {
	x := &schema.Item{
		Text: text,
		Done: false,
		Tags: tags,
	}
	path := fmt.Sprintf("%s/Items/%d", listPath, id)
	st.put(path, x)
}

// processJSONFile saves the contents of the JSON file to the store.
func (st *state) processJSONFile(path string) error {
	vlog.Infof("Loading file %s", path)
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("Can't open %q: %s", path, err)
	}
	defer file.Close()

	lists := make([]*List, 0)
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&lists); err != nil {
		return fmt.Errorf("Can't decode: %s", err)
	}

	st.newTransaction()
	for _, v := range lists {
		st.storeList(v)
	}
	st.commit()
	return nil
}

// main reads the data JSON file and populates the store.
func main() {
	// The client's identity needs to match the Admin ACLs at the empty store
	// (since only the admin can put data). The identity here matches with that
	// used for server.ServerConfig.Admin in todos_stored/main.go. An alternative
	// would be to relax the ACLs on the store.
	rt.Init(veyron2.LocalID(security.FakePrivateID("anonymous")))

	vlog.Infof("Binding to store on %s", storeName)
	st, err := vstore.New(storeName)
	if err != nil {
		vlog.Fatalf("Can't connect to store: %s: %s", storeName, err)
	}
	state := newState(st)

	if err := state.processJSONFile(*dataPath); err != nil {
		vlog.Errorf("Failed to write data: %s", err)
	}
}
