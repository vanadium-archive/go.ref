package server

import (
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/services/security/groups"
	"v.io/v23/verror"
)

type group struct {
	name string
	m    *manager
}

var _ groups.GroupServerMethods = (*group)(nil)

// TODO(sadovsky): verror.NewErrFoo(...) methods do not accept extra arguments,
// so in various places below we call verror.New(verror.ErrFoo, ...) instead.

// TODO(sadovsky): What prevents "mike" from creating group "adam:AllMyDevices"?
// It seems we need either (a) identity providers to manage group servers and
// reserve buckets for users they've blessed, or (b) some way to determine the
// user name from a blessing and enforce that group names start with user names.
func (g *group) Create(call ipc.ServerCall, acl access.Permissions, entries []groups.BlessingPatternChunk) error {
	// Perform AccessList check.
	// TODO(sadovsky): Enable this AccessList check and acquire a lock on the group
	// server AccessList.
	if false {
		if err := g.authorize(call, g.m.acl); err != nil {
			return err
		}
	}
	if acl == nil {
		acl = access.Permissions{}
		blessings, _ := security.BlessingNames(call, security.CallSideRemote)
		if len(blessings) == 0 {
			// The blessings presented by the caller do not give it a name for this
			// operation. We could create a world-accessible group, but it seems safer
			// to return an error.
			return groups.NewErrNoBlessings(call.Context())
		}
		for _, tag := range access.AllTypicalTags() {
			for _, b := range blessings {
				acl.Add(security.BlessingPattern(b), string(tag))
			}
		}
	}
	entrySet := map[groups.BlessingPatternChunk]struct{}{}
	for _, v := range entries {
		entrySet[v] = struct{}{}
	}
	gd := groupData{AccessList: acl, Entries: entrySet}
	if err := g.m.st.Insert(g.name, gd); err != nil {
		// TODO(sadovsky): We are leaking the fact that this group exists. If the
		// client doesn't have access to this group, we should probably return an
		// opaque error. (That's not a great solution either. Having per-user
		// buckets seems like a better solution.)
		if _, ok := err.(*ErrKeyAlreadyExists); ok {
			return verror.New(verror.ErrExist, call.Context(), g.name)
		}
		return verror.New(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

func (g *group) Delete(call ipc.ServerCall, etag string) error {
	return g.readModifyWrite(call, etag, func(gd *groupData, etagSt string) error {
		return g.m.st.Delete(g.name, etagSt)
	})
}

func (g *group) Add(call ipc.ServerCall, entry groups.BlessingPatternChunk, etag string) error {
	return g.update(call, etag, func(gd *groupData) {
		gd.Entries[entry] = struct{}{}
	})
}

func (g *group) Remove(call ipc.ServerCall, entry groups.BlessingPatternChunk, etag string) error {
	return g.update(call, etag, func(gd *groupData) {
		delete(gd.Entries, entry)
	})
}

// TODO(sadovsky): Replace fake implementation with real implementation.
func (g *group) Get(call ipc.ServerCall, req groups.GetRequest, reqEtag string) (res groups.GetResponse, etag string, err error) {
	gd, etag, err := g.getInternal(call)
	if err != nil {
		return groups.GetResponse{}, "", err
	}
	return groups.GetResponse{Entries: gd.Entries}, etag, nil
}

// TODO(sadovsky): Replace fake implementation with real implementation.
func (g *group) Rest(call ipc.ServerCall, req groups.RestRequest, reqEtag string) (res groups.RestResponse, etag string, err error) {
	_, etag, err = g.getInternal(call)
	if err != nil {
		return groups.RestResponse{}, "", err
	}
	return groups.RestResponse{}, etag, nil
}

func (g *group) SetPermissions(call ipc.ServerCall, acl access.Permissions, etag string) error {
	return g.update(call, etag, func(gd *groupData) {
		gd.AccessList = acl
	})
}

func (g *group) GetPermissions(call ipc.ServerCall) (acl access.Permissions, etag string, err error) {
	gd, etag, err := g.getInternal(call)
	if err != nil {
		return nil, "", err
	}
	return gd.AccessList, etag, nil
}

////////////////////////////////////////
// Internal helpers

// Returns a VDL-compatible error.
func (g *group) authorize(call ipc.ServerCall, acl access.Permissions) error {
	// TODO(sadovsky): We ignore the returned error since TypicalTagType is
	// guaranteed to return a valid tagType. It would be nice to have an
	// alternative function that assumes TypicalTagType, since presumably that's
	// the overwhelmingly common case.
	auth, _ := access.PermissionsAuthorizer(acl, access.TypicalTagType())
	if err := auth.Authorize(call); err != nil {
		// TODO(sadovsky): Return NoAccess if appropriate.
		return verror.New(verror.ErrNoExistOrNoAccess, call.Context(), err)
	}
	return nil
}

// Returns a VDL-compatible error. Performs access check.
func (g *group) getInternal(call ipc.ServerCall) (gd groupData, etag string, err error) {
	v, etag, err := g.m.st.Get(g.name)
	if err != nil {
		if _, ok := err.(*ErrUnknownKey); ok {
			// TODO(sadovsky): Return NoExist if appropriate.
			return groupData{}, "", verror.New(verror.ErrNoExistOrNoAccess, call.Context(), g.name)
		}
		return groupData{}, "", verror.New(verror.ErrInternal, call.Context(), err)
	}
	gd, ok := v.(groupData)
	if !ok {
		return groupData{}, "", verror.New(verror.ErrInternal, call.Context(), "bad value for key: "+g.name)
	}
	if err := g.authorize(call, gd.AccessList); err != nil {
		return groupData{}, "", err
	}
	return gd, etag, nil
}

// Returns a VDL-compatible error. Performs access check.
func (g *group) update(call ipc.ServerCall, etag string, fn func(gd *groupData)) error {
	return g.readModifyWrite(call, etag, func(gd *groupData, etagSt string) error {
		fn(gd)
		return g.m.st.Update(g.name, *gd, etagSt)
	})
}

// Returns a VDL-compatible error. Performs access check.
// fn should perform the "modify, write" part of "read, modify, write", and
// should return a Store error.
func (g *group) readModifyWrite(call ipc.ServerCall, etag string, fn func(gd *groupData, etagSt string) error) error {
	// Transaction retry loop.
	for i := 0; i < 3; i++ {
		gd, etagSt, err := g.getInternal(call)
		if err != nil {
			return err
		}
		// Fail early if possible.
		if etag != "" && etag != etagSt {
			return verror.NewErrBadEtag(call.Context())
		}
		if err := fn(&gd, etagSt); err != nil {
			if err, ok := err.(*ErrBadEtag); ok {
				// Retry on etag error if the original etag was empty.
				if etag != "" {
					return verror.NewErrBadEtag(call.Context())
				}
			} else {
				// Abort on non-etag error.
				return verror.New(verror.ErrInternal, call.Context(), err)
			}
		} else {
			return nil
		}
	}
	return groups.NewErrExcessiveContention(call.Context())
}
