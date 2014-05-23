package vsync

// Helpful wrappers to a persistent key/value (K/V) DB used by Veyron Sync.
// The current underlying DB is gkvlite.

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/steveyen/gkvlite"

	"veyron2/vom"
)

type kvdb struct {
	store *gkvlite.Store
	fdesc *os.File
}

type kvtable struct {
	coll *gkvlite.Collection
}

// kvdbOpen opens or creates a K/V DB for the given filename and tables names
// within the DB.  It returns the DB handler and handlers for each table.
func kvdbOpen(filename string, tables []string) (*kvdb, []*kvtable, error) {
	// Open the file and create it if it does not exist.
	fdesc, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, nil, err
	}

	// Initialize the DB (store) and its tables (collections).
	// The store takes ownership of fdesc on success.
	store, err := gkvlite.NewStore(fdesc)
	if err != nil {
		fdesc.Close()
		return nil, nil, err
	}

	flush := false
	tbls := make([]*kvtable, len(tables))

	for i, table := range tables {
		coll := store.GetCollection(table)
		if coll == nil {
			if coll = store.SetCollection(table, nil); coll == nil {
				store.Close()
				return nil, nil, fmt.Errorf("cannot create K/V DB table %s in file %s", table, filename)
			}
			flush = true
		}
		tbls[i] = &kvtable{coll: coll}
	}

	if flush {
		store.Flush() // Flush newly created collections.
	}

	db := &kvdb{store: store, fdesc: fdesc}
	return db, tbls, nil
}

// close closes the given K/V DB.
func (db *kvdb) close() {
	db.store.Close()
	db.fdesc.Close()
}

// flush flushes the given K/V DB to disk.
func (db *kvdb) flush() {
	db.store.Flush()
	db.fdesc.Sync()
}

// set stores (or overwrites) the given key/value pair in the DB table.
func (t *kvtable) set(key string, value interface{}) error {
	val := new(bytes.Buffer)
	if err := vom.NewEncoder(val).Encode(value); err != nil {
		return err
	}
	return t.coll.Set([]byte(key), val.Bytes())
}

// create stores the given key/value pair in the DB table only if
// the key does not already exist.  Otherwise it returns an error.
func (t *kvtable) create(key string, value interface{}) error {
	if t.hasKey(key) {
		return fmt.Errorf("key %s exists", key)
	}
	return t.set(key, value)
}

// update stores the given key/value pair in the DB table only if
// the key already exists.  Otherwise it returns an error.
func (t *kvtable) update(key string, value interface{}) error {
	if !t.hasKey(key) {
		return fmt.Errorf("key %s does not exist", key)
	}
	return t.set(key, value)
}

// get retrieves the value of a key from the DB table.
func (t *kvtable) get(key string, value interface{}) error {
	val, err := t.coll.Get([]byte(key))
	if err != nil {
		return err
	}
	if val == nil {
		return fmt.Errorf("entry %s not found in the K/V DB table", key)
	}
	return vom.NewDecoder(bytes.NewBuffer(val)).Decode(value)
}

// del deletes the entry in the DB table given its key.
func (t *kvtable) del(key string) error {
	_, err := t.coll.Delete([]byte(key))
	return err
}

// hasKey returns true if the given key exists in the DB table.
func (t *kvtable) hasKey(key string) bool {
	item, err := t.coll.GetItem([]byte(key), false)
	return err == nil && item != nil
}

// keyIter iterates over all keys in a DB table invoking the given callback
// function for each one.  The key iterator callback is passed the item key.
func (t *kvtable) keyIter(keyIterCB func(key string)) error {
	return t.coll.VisitItemsAscend(nil, false, func(item *gkvlite.Item) bool {
		keyIterCB(string(item.Key))
		return true
	})
}

// compact compacts the K/V DB file on disk.  It flushs the DB file, creates
// a compact copy of the file under /tmp, then closes the DB, moves the new
// file to replace the old one, then re-opens the new DB file.
func (db *kvdb) compact(filename string, tables []string) (*kvdb, []*kvtable, error) {
	db.store.Flush()

	// Create a unique temporary filename to copy the compact store into.
	prefix := path.Base(filename)
	if prefix == "." || prefix == "/" {
		return nil, nil, fmt.Errorf("invalid DB filename %s", filename)
	}

	fdesc, err := ioutil.TempFile("", prefix)
	if err != nil {
		return nil, nil, err
	}
	tmpfile := fdesc.Name()
	defer os.Remove(tmpfile)
	defer fdesc.Close()

	// Make a compact copy of the store.
	store, err := db.store.CopyTo(fdesc, 0)
	if err == nil {
		err = store.Flush()
	}
	store.Close()
	if err != nil {
		return nil, nil, err
	}

	// Swap the files and re-open the new store.
	if err = os.Rename(tmpfile, filename); err != nil {
		return nil, nil, err
	}

	db.close() // close it, after the rename there is no turning back
	return kvdbOpen(filename, tables)
}
