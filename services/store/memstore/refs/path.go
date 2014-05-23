package refs

import (
	"sync"
	"unsafe"

	"veyron2/storage"
)

// Path represents a path name, interconvertible with storage.PathName, but
// hash-consed.  To maximize sharing, the path is represented in reverse order.
// Because of the hash-consing, this is most appropriate for common path fragments.
// For full names, consider FullPath.
type Path struct {
	hd string // head.
	tl *Path  // tail.
}

type pathHashConsTable struct {
	sync.Mutex
	table map[Path]*Path
}

const (
	TagsDirName = ".tags"
)

var (
	pathTable = &pathHashConsTable{table: make(map[Path]*Path)}
	nilPath   *Path

	tagsDir = NewSingletonPath(TagsDirName)
)

// ComparePaths defines a total order over *Path values, based on pointer
// comparison.  Since paths are hash-consed, this is stable (but arbitrary) with
// a process, but it is not persistent across processes.
func ComparePaths(p1, p2 *Path) int {
	i1 := uintptr(unsafe.Pointer(p1))
	i2 := uintptr(unsafe.Pointer(p2))
	switch {
	case i1 < i2:
		return -1
	case i1 > i2:
		return 1
	default:
		return 0
	}
}

// NewSingletonPath creates a path that has just one component.
func NewSingletonPath(name string) *Path {
	return nilPath.Append(name)
}

// String returns a printable representation of a path.
func (p *Path) String() string {
	if p == nil {
		return ""
	}
	s := p.hd
	for p = p.tl; p != nil; p = p.tl {
		s = p.hd + "/" + s
	}
	return s
}

// Suffix prints the name corresponding to the last n elements
// of the path.
func (p *Path) Suffix(n int) string {
	if p == nil || n == 0 {
		return ""
	}
	s := p.hd
	for i, p := 1, p.tl; i < n && p != nil; i, p = i+1, p.tl {
		s = p.hd + "/" + s
	}
	return s
}

// Append adds a new string component to the end of a path.
func (p *Path) Append(name string) *Path {
	pathTable.Lock()
	defer pathTable.Unlock()
	p1 := Path{hd: name, tl: p}
	if p2, ok := pathTable.table[p1]; ok {
		return p2
	}
	pathTable.table[p1] = &p1
	return &p1
}

// Name returns a storage.PathName corresponding to the path.
func (p *Path) Name() storage.PathName {
	i := p.Len()
	pl := make(storage.PathName, i)
	for ; p != nil; p = p.tl {
		i--
		pl[i] = p.hd
	}
	return pl
}

// Len returns the number of components in the path.
func (p *Path) Len() int {
	i := 0
	for ; p != nil; p = p.tl {
		i++
	}
	return i
}

// Split splits a path into a directory and file part.
func (p *Path) Split() (*Path, string) {
	return p.tl, p.hd
}

// FullPath represents a path name, interconvertible with storage.PathName.
// To maximize sharing, the path is represented in reverse order.
type FullPath Path

// NewSingletonFullPath creates a path that has just one component.
func NewSingletonFullPath(name string) *FullPath {
	return &FullPath{hd: name}
}

// NewFullPathFromName creates a FullPath that represents the same name as path.
func NewFullPathFromName(path storage.PathName) *FullPath {
	var fp *FullPath
	for _, el := range path {
		fp = fp.Append(el)
	}
	return fp
}

// String returns a printable representation of a path.
func (fp *FullPath) String() string {
	return (*Path)(fp).String()
}

// Suffix prints the name corresponding to the last n elements
// of the full path.
func (fp *FullPath) Suffix(n int) string {
	return (*Path)(fp).Suffix(n)
}

// Append adds a new string component to the end of a path.
func (fp *FullPath) Append(name string) *FullPath {
	return &FullPath{hd: name, tl: (*Path)(fp)}
}

// Name returns a storage.PathName corresponding to the path.
func (fp *FullPath) Name() storage.PathName {
	return (*Path)(fp).Name()
}

// Len returns the number of components in the path.
func (fp *FullPath) Len() int {
	return (*Path)(fp).Len()
}

// Split splits a path into a directory and file part.
func (fp *FullPath) Split() (*FullPath, string) {
	return (*FullPath)(fp.tl), fp.hd
}

// AppendPath returns a FullPath that represents the concatenation of fp and p.
func (fp *FullPath) AppendPath(p *Path) *FullPath {
	if p == nil {
		return fp
	}
	return fp.AppendPath(p.tl).Append(p.hd)
}
