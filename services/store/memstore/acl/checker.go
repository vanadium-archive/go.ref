package acl

import (
	"bytes"
	"fmt"

	"veyron2/security"
	"veyron2/storage"
)

// Entry includes an ACL and flags to indicate whether the ACL should be inherited.
type Entry struct {
	ACL       *storage.ACL
	Inherited bool
}

// Set is a set of Entries, indexed by their IDs.
type Set map[storage.ID]Entry

// Checker is used to check if a principal matches ACLs extracted from the tags
// applied to objects in a store.
//
// While walking through a path as part of resolving a cell, call Update for each
// component of the path.  Checker will then keep track of the inherited ACLs
// for that path.
type Checker struct {
	cache     *Cache
	principal security.PublicID
	acls      Set
}

// NewChecker constructs a new Checker and returns it.
func NewChecker(cache *Cache, clientID security.PublicID, acls Set) *Checker {
	// Copy the Set.
	cp := make(Set)
	for id, acl := range acls {
		cp[id] = acl
	}
	return &Checker{cache: cache, principal: clientID, acls: cp}
}

// Copy, so that updates do not affect the original.
func (c Checker) Copy() *Checker {
	acls := make(Set)
	for id, acl := range c.acls {
		acls[id] = acl
	}
	c.acls = acls
	return &c
}

// Update is called for each step in a path traversal to update the
// Checker using the TagList associated with a value in the store.
func (c *Checker) Update(tags storage.TagList) {
	// The caller has just made one step deeper into the path.  The non-inherited
	// ACLs are no longer relevant, so prune them.
	for id, entry := range c.acls {
		if !entry.Inherited {
			delete(c.acls, id)
		}
	}

	// Add the new ACLc.
	for _, tag := range tags {
		switch tag.Op {
		case storage.RemoveACL:
			delete(c.acls, tag.ACL)
		case storage.AddACL:
			if acl := c.cache.get(tag.ACL); acl != nil {
				c.acls[tag.ACL] = Entry{ACL: acl}
			}
		case storage.AddInheritedACL:
			if acl := c.cache.get(tag.ACL); acl != nil {
				c.acls[tag.ACL] = Entry{ACL: acl, Inherited: true}
			}
		}
	}
}

// IsAllowed returns true iff the current acls allow the principal to use a
// label.
func (c *Checker) IsAllowed(label security.Label) bool {
	for _, entry := range c.acls {
		for key, labels := range entry.ACL.Contents {
			if labels.HasLabel(label) {
				if c.principal.Match(key) {
					return true
				}
			}
		}
	}
	return false
}

// IsEqual returns true iff the checkers are exactly equivalent, containing the same ACLs.
func (c1 *Checker) IsEqual(c2 *Checker) bool {
	if c1.cache != c2.cache || c1.principal != c2.principal || len(c1.acls) != len(c2.acls) {
		return false
	}

	for id, _ := range c1.acls {
		if _, ok := c2.acls[id]; !ok {
			return false
		}
	}
	return true
}

func (c Checker) String() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Checker{principal:%q", c.principal)
	for p, l := range c.acls {
		fmt.Fprintf(&buf, ", %s:%s", p, l)
	}
	buf.WriteRune('}')
	return buf.String()
}

func (e Entry) String() string {
	if e.Inherited {
		return "[Inherited]" + e.ACL.String()
	}
	return e.ACL.String()
}
