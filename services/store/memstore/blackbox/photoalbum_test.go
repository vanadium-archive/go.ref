package blackbox

import (
	"runtime"
	"testing"

	"veyron/services/store/memstore"
	"veyron/services/store/service"

	"veyron2/storage"
	"veyron2/vom"
)

func init() {
	vom.Register(&Dir{})
	vom.Register(&User{})
	vom.Register(&Photo{})
	vom.Register(&Edit{})
	vom.Register(&Album{})
}

// Dir is a "directory" containg a dictionaries of entries.
type Dir struct{}

// User represents a "user", with a username and a "home" directory.
// The name of the user is part of the path to the object.
type User struct {
	Dir
	SSN int
}

// Photo represents an image.  It contains the Object name for the data,
// stored elsewhere on some content server.
type Photo struct {
	Dir
	Comment string
	Content string // Object name
	Edits   []Edit
}

// Edit is an edit to a Photo.
type Edit struct {
	// ...
}

// Album is a photoalbum.
type Album struct {
	Title  string
	Photos map[string]storage.ID
}

func newDir() *Dir {
	return &Dir{}
}

func newUser(ssn int) *User {
	return &User{SSN: ssn}
}

func newAlbum(title string) *Album {
	return &Album{Title: title}
}

func newPhoto(content, comment string, edits ...Edit) *Photo {
	return &Photo{Content: content, Comment: comment}
}

func getPhoto(t *testing.T, st *memstore.Store, tr service.Transaction, path string) *Photo {
	_, file, line, _ := runtime.Caller(1)
	v := Get(t, st, tr, path).Value
	p, ok := v.(*Photo)
	if !ok {
		t.Fatalf("%s(%d): %s: not a Photo: %#v", file, line, path, v)
	}
	return p
}

func TestPhotoAlbum(t *testing.T) {
	st, err := memstore.New(rootPublicID, "")
	if err != nil {
		t.Fatalf("memstore.New() failed: %v", err)
	}

	// Create directories.
	{
		tr := memstore.NewTransaction()
		Put(t, st, tr, "/", newDir())
		Put(t, st, tr, "/Users", newDir())
		Put(t, st, tr, "/Users/jyh", newUser(1234567890))
		Put(t, st, tr, "/Users/jyh/ByDate", newDir())
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01", newDir())
		Put(t, st, tr, "/Users/jyh/Albums", newDir())
		Commit(t, tr)
	}

	// Add some photos by date.
	{
		p1 := newPhoto("/global/contentd/DSC1000.jpg", "Half Dome")
		p2 := newPhoto("/global/contentd/DSC1001.jpg", "I don't want to hike")
		p3 := newPhoto("/global/contentd/DSC1002.jpg", "Crying kids")
		p4 := newPhoto("/global/contentd/DSC1003.jpg", "Ice cream")
		p5 := newPhoto("/global/contentd/DSC1004.jpg", "Let's go home")

		tr := memstore.NewTransaction()
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01/09:00", p1)
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01/09:15", p2)
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01/09:16", p3)
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01/10:00", p4)
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01/10:05", p5)
		Commit(t, tr)
	}

	// Add an Album with some of the photos.
	{
		tr := memstore.NewTransaction()
		Put(t, st, tr, "/Users/jyh/Albums/Yosemite", newAlbum("Yosemite selected photos"))
		e5 := Get(t, st, tr, "/Users/jyh/ByDate/2014_01_01/10:05")
		Put(t, st, tr, "/Users/jyh/Albums/Yosemite/Photos/1", e5.Stat.ID)
		e3 := Get(t, st, tr, "/Users/jyh/ByDate/2014_01_01/09:16")
		Put(t, st, tr, "/Users/jyh/Albums/Yosemite/Photos/2", e3.Stat.ID)
		Commit(t, tr)
	}

	// Verify some of the photos.
	{
		p1 := getPhoto(t, st, nil, "/Users/jyh/ByDate/2014_01_01/09:00")
		if p1.Comment != "Half Dome" {
			t.Errorf("Expected %q, got %q", "Half Dome", p1.Comment)
		}
	}

	{
		p3 := getPhoto(t, st, nil, "/Users/jyh/Albums/Yosemite/Photos/2")
		if p3.Comment != "Crying kids" {
			t.Errorf("Expected %q, got %q", "Crying kids", p3.Comment)
		}
	}

	// Update p3.Comment = "Happy"
	{
		tr := memstore.NewTransaction()
		p3 := getPhoto(t, st, tr, "/Users/jyh/ByDate/2014_01_01/09:16")
		p3.Comment = "Happy"
		Put(t, st, tr, "/Users/jyh/ByDate/2014_01_01/09:16", p3)
		Commit(t, tr)
	}

	// Verify that the photo in the album has also changed.
	{
		p3 := getPhoto(t, st, nil, "/Users/jyh/Albums/Yosemite/Photos/2")
		if p3.Comment != "Happy" {
			t.Errorf("Expected %q, got %q", "Happy", p3.Comment)
		}
	}
}
