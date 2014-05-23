package refs

import (
	"testing"

	"veyron2/storage"
)

func storePathEqual(p1, p2 storage.PathName) bool {
	if len(p1) != len(p2) {
		return false
	}
	for i, s := range p1 {
		if p2[i] != s {
			return false
		}
	}
	return true
}

func TestPath(t *testing.T) {
	p1 := NewSingletonPath("a")
	p2 := NewSingletonPath("a")
	if p1 != p2 {
		t.Errorf("Paths should be identical")
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a"}) {
		t.Errorf("Got %s, expected 'a'", p2.Name())
	}
	if p1.Len() != 1 {
		t.Errorf("Got length %d, expected 1", p1.Len())
	}

	p1 = p1.Append("b")
	p2 = p2.Append("b")
	if p1 != p2 {
		t.Errorf("Paths should be identical")
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a", "b"}) {
		t.Errorf("Got %s, expected 'a/b'", p2.Name())
	}

	p1 = p1.Append("c")
	p2 = p2.Append("c")
	if p1 != p2 {
		t.Errorf("Paths should be identical")
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a", "b", "c"}) {
		t.Errorf("Got %s, expected 'a/b/c'", p2.Name())
	}

	p1 = p1.Append("d")
	p2 = p2.Append("e")
	if p1 == p2 {
		t.Errorf("Paths should not be identical, %s, %s", p1, p2)
	}
	if !storePathEqual(p1.Name(), storage.PathName{"a", "b", "c", "d"}) {
		t.Errorf("Got %s, expected 'a/b/c.d'", p1.Name())
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a", "b", "c", "e"}) {
		t.Errorf("Got %s, expected 'a/b/c/e'", p2.Name())
	}
}

func TestPathSuffix(t *testing.T) {
	p1 := NewSingletonPath("a")
	if s := p1.Suffix(0); s != "" {
		t.Errorf("Got '%s' expected ''", s)
	}
	if s := p1.Suffix(1); s != "a" {
		t.Errorf("Got '%s' expected 'a'", s)
	}
	if s := p1.Suffix(100); s != "a" {
		t.Errorf("Got '%s' expected 'a'", s)
	}

	p2 := p1.Append("b").Append("c").Append("d")
	if s := p2.Suffix(0); s != "" {
		t.Errorf("Got '%s' expected ''", s)
	}
	if s := p2.Suffix(1); s != "d" {
		t.Errorf("Got '%s' expected 'd'", s)
	}
	if s := p2.Suffix(2); s != "c/d" {
		t.Errorf("Got '%s' expected 'c/d'", s)
	}
	if s := p2.Suffix(3); s != "b/c/d" {
		t.Errorf("Got '%s' expected 'b/c/d'", s)
	}
	if s := p2.Suffix(4); s != "a/b/c/d" {
		t.Errorf("Got '%s' expected 'a/b/c/d'", s)
	}
	if s := p2.Suffix(100); s != "a/b/c/d" {
		t.Errorf("Got '%s' expected 'a/b/c/d'", s)
	}
}

func TestFullPath(t *testing.T) {
	p := NewSingletonFullPath("a")
	if s := p.String(); s != "a" {
		t.Errorf("Got %s expected 'a'", s)
	}

	p = p.Append("b").Append("c")
	if s := p.String(); s != "a/b/c" {
		t.Errorf("Got %s expected 'a/b/c'", s)
	}

	suffix := NewSingletonPath("d").Append("e")
	p = p.AppendPath(suffix)
	if s := p.String(); s != "a/b/c/d/e" {
		t.Errorf("Got %s expected 'a/b/c/d/e'", s)
	}

	p = NewFullPathFromName(storage.PathName{"a", "b", "c"})
	if s := p.String(); s != "a/b/c" {
		t.Errorf("Got %s expected 'a/b/c'", s)
	}
}

func TestFullPathSuffix(t *testing.T) {
	p1 := NewSingletonFullPath("a")
	if s := p1.Suffix(0); s != "" {
		t.Errorf("Got '%s' expected ''", s)
	}
	if s := p1.Suffix(1); s != "a" {
		t.Errorf("Got '%s' expected 'a'", s)
	}
	if s := p1.Suffix(100); s != "a" {
		t.Errorf("Got '%s' expected 'a'", s)
	}

	p2 := p1.Append("b").Append("c").Append("d")
	if s := p2.Suffix(0); s != "" {
		t.Errorf("Got '%s' expected ''", s)
	}
	if s := p2.Suffix(1); s != "d" {
		t.Errorf("Got '%s' expected 'd'", s)
	}
	if s := p2.Suffix(2); s != "c/d" {
		t.Errorf("Got '%s' expected 'c/d'", s)
	}
	if s := p2.Suffix(3); s != "b/c/d" {
		t.Errorf("Got '%s' expected 'b/c/d'", s)
	}
	if s := p2.Suffix(4); s != "a/b/c/d" {
		t.Errorf("Got '%s' expected 'a/b/c/d'", s)
	}
	if s := p2.Suffix(100); s != "a/b/c/d" {
		t.Errorf("Got '%s' expected 'a/b/c/d'", s)
	}
}
