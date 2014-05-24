// mdb_init is a tool to initialize the store with an initial database.  This is
// really for demo purposes; in a real database, the contents would be persistant.
//
// The contents are loaded from JSON format.  See mdb/templates/contents.json
// for the actual input.
//
// Since JSON doesn't support all of the store types, there is a translation
// phase, where the contents are loaded into a string form, then converted to
// the mdb/schema schema.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"veyron/examples/storage/mdb/schema"
	"veyron2/rt"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/storage/vstore/primitives"
	"veyron2/vlog"
)

var (
	storeName    string
	templatesDir = flag.String("templates", "templates", "Name of the templates directory")

	loadContents  = flag.Bool("load-contents", false, "Load contents")
	loadTemplates = flag.Bool("load-templates", false, "Load templates")
	loadAll       = flag.Bool("load-all", false, "Load everything")
)

// Movie is the JSON representation for schema.Movie.
type Movie struct {
	Image       string
	Title       string
	Summary     string
	Language    string
	ReleaseDate string
	Runtime     uint
	Genre       string
	Director    string
}

// Part is the JSON representation for schema.Part.
type Part struct {
	Movie     string
	Actor     string
	Character string
}

// Person is the JSON representation for schema.Person.
type Person struct {
	Name      string
	BirthDate string
	Image     string
}

// Review is the JSON representation for schema.Review.
type Review struct {
	Movie  string
	Rating uint8
	Text   string
}

// Contents is the JSON object containing the initial store state.
type Contents struct {
	People  map[string]*Person
	Parts   map[string]*Part
	Movies  map[string]*Movie
	Reviews map[string]*Review
}

// state is the initial store state.
type state struct {
	store       storage.Store
	transaction storage.Transaction
	idTable     map[string]*value
}

// value holds the ID and name of a stored value.
type value struct {
	id   storage.ID
	path string
}

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
	flag.StringVar(&storeName, "store", dir, "Name of the Veyron store")
}

// parseDate converts from a string data <day>/<month>/<year> UTC to a numeric
// date represented in nanoseconds since the Unix epoch.
func parseDate(date string) (int64, error) {
	parts := strings.Split(date, "/")
	if len(parts) != 3 {
		return 0, fmt.Errorf("Bad date: %s", date)
	}
	day, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	year, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, err
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return t.UnixNano(), nil
}

// parseDuration converts from seconds to nanoseconds.
func parseDuration(dur uint) int64 {
	return int64(dur) * 1000000000
}

// newState returns a fresh state.
func newState(st storage.Store) *state {
	return &state{store: st, idTable: make(map[string]*value)}
}

// find fetches a value from the store.
func (st *state) find(name string) *value {
	return st.idTable[name]
}

// put adds a value to the store, creating the path to the value if it doesn't
// already exist.
func (st *state) put(path string, v interface{}) {
	vlog.Infof("Storing %q = %+v", path, v)
	st.makeParentDirs(path)
	if _, err := st.store.Bind(path).Put(st.transaction, v); err != nil {
		vlog.Infof("put failed: %s: %s", path, err)
		return
	}
}

// putNamed adds a value to the store, similar to put, but it also adds the
// value to the idTable using a symbolic name.
func (st *state) putNamed(name, path string, v interface{}) {
	vlog.Infof("Storing %s: %q = %+v", name, path, v)
	st.makeParentDirs(path)
	s, err := st.store.Bind(path).Put(st.transaction, v)
	if err != nil {
		vlog.Infof("Put failed: %s: %s", path, err)
		return
	}
	st.idTable[name] = &value{id: s.ID, path: path}
}

// makeParentDirs creates the directories in a path if they do not already
// exist.
func (st *state) makeParentDirs(path string) {
	l := strings.Split(path, "/")
	for i, _ := range l {
		prefix := filepath.Join(l[:i]...)
		o := st.store.Bind(prefix)
		if exist, err := o.Exists(st.transaction); err != nil {
			vlog.Infof("Error checking existence at %q: %s", prefix, err)
		} else if !exist {
			if _, err := o.Put(st.transaction, &schema.Dir{}); err != nil {
				vlog.Infof("Error creating parent %q: %s", prefix, err)
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
		vlog.Infof("Failed to commit transaction: %s", err)
	}
	st.transaction = nil
}

// storeContents saves each of the values in the Contents to the store.
func (st *state) storeContents(c *Contents) {
	for name, p := range c.People {
		st.storePerson(name, p)
	}
	for name, m := range c.Movies {
		st.storeMovie(name, m)
	}
	for name, p := range c.Parts {
		st.storePart(name, p)
	}
	for name, r := range c.Reviews {
		st.storeReview(name, r)
	}
}

// storePerson saves a schema.Person to the store with the name /people/<Name>.
func (st *state) storePerson(name string, p *Person) {
	date, err := parseDate(p.BirthDate)
	if err != nil {
		vlog.Infof("Invalid date: %s", err)
		return
	}
	x := &schema.Person{
		Name:      p.Name,
		BirthDate: date,
		Image:     p.Image,
	}
	path := "/people/" + p.Name
	st.putNamed(name, path, x)
}

// storeMovie saves a schema.Movie to the store with the name /movie/<Title>.
func (st *state) storeMovie(name string, m *Movie) {
	releaseDate, err := parseDate(m.ReleaseDate)
	if err != nil {
		vlog.Infof("Invalid date: %s", err)
		return
	}
	runtime := parseDuration(m.Runtime)
	director := st.find(m.Director)
	if director == nil {
		vlog.Infof("Can't find director: %s", m.Director)
		return
	}
	x := &schema.Movie{
		Image:       m.Image,
		Title:       m.Title,
		Summary:     m.Summary,
		Language:    m.Language,
		ReleaseDate: releaseDate,
		Runtime:     runtime,
		Genre:       m.Genre,
		Director:    director.id,
	}
	path := "/movies/" + x.Title
	st.putNamed(name, path, x)
}

// storePart saves a schema.Part to the store, with the name /movie/<Title>/Cast/<id>.
func (st *state) storePart(name string, p *Part) {
	movie := st.find(p.Movie)
	if movie == nil {
		vlog.Infof("Can't find movie %s", p.Movie)
		return
	}
	actor := st.find(p.Actor)
	if movie == nil {
		vlog.Infof("Can't find actor %s", p.Actor)
		return
	}
	x := &schema.Part{
		Actor:     actor.id,
		Character: p.Character,
	}
	path := fmt.Sprintf("%s/Cast/%s", movie.path, name)
	st.putNamed(name, path, x)
}

// storeReview saves a review to the store, with the name /movie/<Title>/Reviews/<id>.
func (st *state) storeReview(name string, r *Review) {
	movie := st.find(r.Movie)
	if movie == nil {
		vlog.Infof("Can't find movie %s", r.Movie)
		return
	}
	x := &schema.Review{
		Rating: r.Rating,
		Text:   r.Text,
	}
	path := fmt.Sprintf("%s/Reviews/%s", movie.path, name)
	st.putNamed(name, path, x)
}

// processJSONFile saves the contents of the JSON file to the store.
func (st *state) processJSONFile(path string) error {
	vlog.Infof("Loading file %s", path)
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("Can't open %q: %s", path, err)
	}
	defer file.Close()

	contents := &Contents{}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(contents); err != nil {
		return fmt.Errorf("Can't decode: %s", err)
	}

	st.newTransaction()
	st.storeContents(contents)
	st.commit()
	return nil
}

// processTemplateFile saves a template file to the store as a string.  The name
// is /templates/<path> where <path> is the filesystem path to the template
// file.
func (st *state) processTemplateFile(path, name string) error {
	vlog.Infof("Adding template %s", path)
	s, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Can't read %q: %s", path, err)
	}

	templateName := filepath.ToSlash("templates" + name)
	st.newTransaction()
	st.put(templateName, string(s))
	st.commit()
	return nil
}

// processRawFile saves the file contents to the store as a string, using the
// filesystem path as the store name.
func (st *state) processRawFile(path, name string) error {
	vlog.Infof("Adding raw file %s", path)
	s, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Can't read %q: %s", path, err)
	}

	st.newTransaction()
	st.put(name, string(s))
	st.commit()
	return nil
}

// processFile stores the contens of the file to the store.
func (st *state) processFile(path, name string) error {
	switch filepath.Ext(path) {
	case ".json":
		if *loadAll || *loadContents {
			return st.processJSONFile(path)
		}
	case ".tmpl":
		if *loadAll || *loadTemplates {
			return st.processTemplateFile(path, strings.TrimSuffix(name, ".tmpl"))
		}
	case ".css":
		if *loadAll || *loadTemplates {
			return st.processRawFile(path, name)
		}
	}
	return nil
}

// main reads all the files in the templates directory and adds them to the
// store.
func main() {
	// The client's identity needs to match the Admin ACLs at the empty
	// store (since only the admin can put data).  The identity here
	// matches with that used for server.ServerConfig.Admin in
	// mdb_stored/main.go.  An alternative would be to relax the ACLs on
	// the store.
	rt.Init()

	vlog.Infof("Binding to store on %s", storeName)
	st, err := vstore.New(storeName)
	if err != nil {
		vlog.Fatalf("Can't connect to store: %s: %s", storeName, err)
	}
	state := newState(st)

	// Store all templates.
	filepath.Walk(*templatesDir, func(path string, _ os.FileInfo, _ error) error {
		err := state.processFile(path, strings.TrimPrefix(path, *templatesDir))
		if err != nil {
			vlog.Infof("%s: %s", path, err)
		}
		return err
	})
}
