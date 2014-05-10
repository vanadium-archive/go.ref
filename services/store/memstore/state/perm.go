package state

import (
	"veyron/services/store/memstore/acl"

	"veyron2/security"
	"veyron2/storage"
)

var (
	// adminACLID is the storage.ID used for the administrator's default ACL.
	AdminACLID = storage.ID{0}

	// everyoneACLID is the storage.ID used for the default ACL for non-administrators.
	EveryoneACLID = storage.ID{1}

	// uidTagList is the storage.TagList for the /uid directory.  It ensures that
	// /uid/* is accessible only to the administrators of the storage.
	//
	// TODO(jyh): Consider having an actual /uid object, so that the
	// administrator could configure permissions on it.
	uidTagList = storage.TagList{storage.Tag{Op: storage.RemoveACL, ACL: EveryoneACLID}}
)

// makeDefaultACLSet returns the default ACL for the store, allowing admin
// universal access, and everyone else gets readonly access.
func makeDefaultACLSet(admin security.PublicID) acl.Set {
	adminContents := security.ACL{}
	for _, name := range admin.Names() {
		adminContents[security.PrincipalPattern(name)] = security.LabelSet(security.ReadLabel | security.WriteLabel | security.AdminLabel)
	}
	adminACL := &storage.ACL{
		Name:     "admin",
		Contents: adminContents,
	}
	everyoneACL := &storage.ACL{
		Name:     "everyone",
		Contents: security.ACL{security.AllPrincipals: security.LabelSet(security.ReadLabel)},
	}
	return acl.Set{
		AdminACLID:    acl.Entry{ACL: adminACL, Inherited: true},
		EveryoneACLID: acl.Entry{ACL: everyoneACL, Inherited: true},
	}
}

// newPermChecker returns a new acl.Checker in the current state.
func (sn *snapshot) newPermChecker(pid security.PublicID) *acl.Checker {
	return acl.NewChecker(&sn.aclCache, pid, sn.defaultACLSet)
}

// makeFindACLFunc returns a function to fetch ACL values from the storage.
func (sn *snapshot) makeFindACLFunc() acl.FindFunc {
	return func(id storage.ID) *storage.ACL {
		v, ok := sn.idTable.Get(&Cell{ID: id})
		if !ok {
			return nil
		}
		x := v.(*Cell).Value
		if acl, ok := x.(*storage.ACL); ok {
			return acl
		}
		if acl, ok := x.(storage.ACL); ok {
			return &acl
		}
		return nil
	}
}
