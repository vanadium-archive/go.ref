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
		t.Errorf("Expected 'a', got %s", p2.Name())
	}
	if p1.Len() != 1 {
		t.Errorf("Length should be 1, got %d", p1.Len())
	}

	p1 = p1.Append("b")
	p2 = p2.Append("b")
	if p1 != p2 {
		t.Errorf("Paths should be identical")
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a", "b"}) {
		t.Errorf("Expected 'a/b', got %s", p2.Name())
	}

	p1 = p1.Append("c")
	p2 = p2.Append("c")
	if p1 != p2 {
		t.Errorf("Paths should be identical")
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a", "b", "c"}) {
		t.Errorf("Expected 'a/b/c', got %s", p2.Name())
	}

	p1 = p1.Append("d")
	p2 = p2.Append("e")
	if p1 == p2 {
		t.Errorf("Paths should not be identical, %s, %s", p1, p2)
	}
	if !storePathEqual(p1.Name(), storage.PathName{"a", "b", "c", "d"}) {
		t.Errorf("Expected 'a/b/c.d', got %s", p1.Name())
	}
	if !storePathEqual(p2.Name(), storage.PathName{"a", "b", "c", "e"}) {
		t.Errorf("Expected 'a/b/c/e', got %s", p2.Name())
	}
}

func TestFullPath(t *testing.T) {
	p := NewSingletonFullPath("a")
	if s := p.String(); s != "a" {
		t.Errorf("Expected 'a' got %s", s)
	}

	p = p.Append("b").Append("c")
	if s := p.String(); s != "a/b/c" {
		t.Errorf("Expected 'a/b/c' got %s", s)
	}

	suffix := NewSingletonPath("d").Append("e")
	p = p.AppendPath(suffix)
	if s := p.String(); s != "a/b/c/d/e" {
		t.Errorf("Expected 'a/b/c/d/e' got %s", s)
	}

	p = NewFullPathFromName(storage.PathName{"a", "b", "c"})
	if s := p.String(); s != "a/b/c" {
		t.Errorf("Expected 'a/b/c' got %s", s)
	}
}
