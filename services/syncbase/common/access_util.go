// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package common

import (
	"sort"
	"strings"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/store"
)

// ValidatePerms does basic sanity checking on the provided perms:
// - Perms can contain only tags in the provided whitelist.
// - At least one admin must be included to avoid permanently losing access.
func ValidatePerms(ctx *context.T, perms access.Permissions, allowTags []access.Tag) error {
	// Perms cannot be empty or nil.
	if len(perms) == 0 {
		return NewErrPermsEmpty(ctx)
	}
	// Perms cannot contain any tags not in the allowTags whitelist.
	allowTagsSet := make(map[string]struct{}, len(allowTags))
	for _, tag := range allowTags {
		allowTagsSet[string(tag)] = struct{}{}
	}
	var disallowedTags []string
	for tag, _ := range perms {
		if _, ok := allowTagsSet[tag]; !ok {
			disallowedTags = append(disallowedTags, tag)
		}
	}
	if len(disallowedTags) > 0 {
		sort.Strings(disallowedTags)
		return NewErrPermsDisallowedTags(ctx, disallowedTags, access.TagStrings(allowTags...))
	}
	// Perms must include at least one Admin.
	// TODO(ivanpi): More sophisticated admin verification, e.g. check that NotIn
	// doesn't blacklist all possible In matches.
	if adminAcl, ok := perms[string(access.Admin)]; !ok || len(adminAcl.In) == 0 {
		return NewErrPermsNoAdmin(ctx)
	}
	// TODO(ivanpi): Check that perms are enforceable? It would make validation
	// context-dependent.
	return nil
}

// AnyOfTagsAuthorizer provides an authorizer that allows blessings matching any
// pattern in perms corresponding to any of the provided tags.
func AnyOfTagsAuthorizer(tags []access.Tag, perms access.Permissions) *anyOfTagsAuthorizer {
	return &anyOfTagsAuthorizer{
		tags:  tags,
		perms: perms,
	}
}

type anyOfTagsAuthorizer struct {
	tags  []access.Tag
	perms access.Permissions
}

func (a *anyOfTagsAuthorizer) Authorize(ctx *context.T, call security.Call) error {
	blessings, invalid := security.RemoteBlessingNames(ctx, call)
	for _, tag := range a.tags {
		if acl, exists := a.perms[string(tag)]; exists && acl.Includes(blessings...) {
			// At least one tag in the set had a matching pattern.
			return nil
		}
	}
	// None of the tags had a matching pattern. Deny access.
	return verror.New(verror.ErrNoAccess, ctx,
		access.NewErrNoPermissions(ctx, blessings, invalid, strings.Join(access.TagStrings(a.tags...), " ∨ ")))
}

// CheckImplicitPerms performs an authorization check against the implicit
// permissions derived from the blessing pattern in the Id. It returns the
// generated implicit perms or an authorization error.
// TODO(ivanpi): Change to check against the specific blessing used for signing
// instead of any blessing in call.Security().
func CheckImplicitPerms(ctx *context.T, call rpc.ServerCall, id wire.Id, allowedTags []access.Tag) (access.Permissions, error) {
	implicitPerms := access.Permissions{}.Add(security.BlessingPattern(id.Blessing), access.TagStrings(allowedTags...)...)
	// Note, allowedTags is expected to contain access.Admin.
	if err := AnyOfTagsAuthorizer([]access.Tag{access.Admin}, implicitPerms).Authorize(ctx, call.Security()); err != nil {
		return nil, verror.New(wire.ErrUnauthorizedCreateId, ctx, id.Blessing, id.Name, err)
	}
	return implicitPerms, nil
}

// PermserData is persistent metadata about an object, including perms.
type PermserData interface {
	// GetPerms returns the perms for the object.
	GetPerms() access.Permissions
}

// Permser is an object in the hierarchy that supports retrieving perms and
// authorizing access to existence checks.
type Permser interface {
	// GetDataWithExistAuth must return a nil error only if the object exists and
	// the caller is authorized to know it (Resolve access up to the parent and
	// any access tag on self, or Resolve access up to grandparent and Read or
	// Write on parent). Otherwise, the returned error must not leak existence
	// data (ErrNoExistOrNoAccess must be returned instead of more specific
	// errors such as ErrNoExist if the caller is not authorized to know about
	// an object's existence).
	// If the error is nil, PermserData must be populated with object metadata
	// loaded from the store and the method must return perms of the object's
	// parent and the object itself.
	// A typical implementation calls GetPermsWithExistAndParentResolveAuth on
	// the object's parent, followed by GetDataWithExistAuthStep.
	GetDataWithExistAuth(ctx *context.T, call rpc.ServerCall, st store.StoreReader, v PermserData) (parentPerms, perms access.Permissions, existErr error)

	// PermserData returns a zero-value PermserData for this object.
	PermserData() PermserData
}

// getDataWithExistAndParentResolveAuth is equivalent to
// GetPermsWithExistAndParentResolveAuth, in addition populating the loaded
// PermserData into v.
func getDataWithExistAndParentResolveAuth(ctx *context.T, call rpc.ServerCall, at Permser, st store.StoreReader, v PermserData) (access.Permissions, error) {
	parentPerms, perms, existErr := at.GetDataWithExistAuth(ctx, call, st, v)
	if existErr != nil {
		return nil, existErr
	}
	return perms, AnyOfTagsAuthorizer([]access.Tag{access.Resolve}, parentPerms).Authorize(ctx, call.Security())
}

// GetPermsWithExistAndParentResolveAuth returns a nil error only if the object
// exists, the client is authorized to know it and has resolve access on all
// objects up to and including this object's parent.
func GetPermsWithExistAndParentResolveAuth(ctx *context.T, call rpc.ServerCall, at Permser, st store.StoreReader) (access.Permissions, error) {
	return getDataWithExistAndParentResolveAuth(ctx, call, at, st, at.PermserData())
}

// GetDataWithAuth is equivalent to GetPermsWithAuth, in addition populating
// the loaded PermserData into v.
func GetDataWithAuth(ctx *context.T, call rpc.ServerCall, at Permser, tags []access.Tag, st store.StoreReader, v PermserData) (access.Permissions, error) {
	perms, existErr := getDataWithExistAndParentResolveAuth(ctx, call, at, st, v)
	if existErr != nil {
		return nil, existErr
	}
	return perms, AnyOfTagsAuthorizer(tags, perms).Authorize(ctx, call.Security())
}

// GetPermsWithAuth returns a nil error only if the client has exist and parent
// resolve access (see GetPermsWithExistAndParentResolveAuth) as well as at
// least one of the specified tags on the object itself.
func GetPermsWithAuth(ctx *context.T, call rpc.ServerCall, at Permser, tags []access.Tag, st store.StoreReader) (access.Permissions, error) {
	return GetDataWithAuth(ctx, call, at, tags, st, at.PermserData())
}

// GetDataWithExistAuthStep is a helper intended for use in GetDataWithExistAuth
// implementations. It assumes Resolve access up to and including the object's
// grandparent. It loads the object's metadata from the store into v, returning
// ErrNoExistOrNoAccess, ErrNoExist or other errors when appropriate; if the
// caller is not authorized for exist access, ErrNoExistOrNoAccess is always
// returned. If a nil StoreReader is passed in, the object is assumed to not
// exist.
func GetDataWithExistAuthStep(ctx *context.T, call rpc.ServerCall, name string, parentPerms access.Permissions, st store.StoreReader, k string, v PermserData) error {
	if st == nil {
		return fuzzifyErrorForExists(ctx, call, name, parentPerms, nil, verror.New(verror.ErrNoExist, ctx, name))
	}
	if getErr := store.Get(ctx, st, k, v); getErr != nil {
		if verror.ErrorID(getErr) == verror.ErrNoExist.ID {
			getErr = verror.New(verror.ErrNoExist, ctx, name)
		}
		return fuzzifyErrorForExists(ctx, call, name, parentPerms, nil, getErr)
	}
	return fuzzifyErrorForExists(ctx, call, name, parentPerms, v.GetPerms(), nil)
}

// fuzzifyErrorForExists passes through the original error only if the caller is
// authorized for exist access. Otherwise, ErrNoExistOrNoAccess is returned
// instead. It assumes Resolve access up to and including the object's
// grandparent.
func fuzzifyErrorForExists(ctx *context.T, call rpc.ServerCall, name string, parentPerms, perms access.Permissions, originalErr error) error {
	if parentRWErr := AnyOfTagsAuthorizer([]access.Tag{access.Read, access.Write}, parentPerms).Authorize(ctx, call.Security()); parentRWErr == nil {
		// Read or Write on parent, return original error.
		return originalErr
	}
	fuzzyErr := verror.New(verror.ErrNoExistOrNoAccess, ctx, name)
	if perms == nil {
		// Exit early if object does not exist - caller cannot have any perms on it.
		return fuzzyErr
	}
	// No Read or Write on parent, caller must have both Resolve on parent and at
	// least one tag on the object itself to get the original error.
	if parentXErr := AnyOfTagsAuthorizer([]access.Tag{access.Resolve}, parentPerms).Authorize(ctx, call.Security()); parentXErr != nil {
		return fuzzyErr
	}
	if selfAnyErr := AnyOfTagsAuthorizer(access.AllTypicalTags(), perms).Authorize(ctx, call.Security()); selfAnyErr != nil {
		return fuzzyErr
	}
	return originalErr
}

// ErrorToExists converts the error returned from GetDataWithExistAuth into
// the Exists RPC result, suppressing ErrNoExist.
func ErrorToExists(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if verror.ErrorID(err) == verror.ErrNoExist.ID {
		return false, nil
	}
	return false, err
}
