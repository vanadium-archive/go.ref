// This file was auto-generated by the vanadium vdl tool.
// Source: errors.vdl

package ipc

import (
	// VDL system imports
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/verror"

	// VDL user imports
	"v.io/v23/security"
)

var (
	ErrInvalidBlessings = verror.Register("v.io/x/ref/profiles/internal/ipc.InvalidBlessings", verror.NoRetry, "{1:}{2:} All valid blessings for this request: {3} (rejected {4}) are disallowed by the policy {5} (rejected {6})")
	// Internal errors.
	errBadRequest        = verror.Register("v.io/x/ref/profiles/internal/ipc.badRequest", verror.NoRetry, "{1:}{2:} failed to decode request: {3}")
	errBadNumInputArgs   = verror.Register("v.io/x/ref/profiles/internal/ipc.badNumInputArgs", verror.NoRetry, "{1:}{2:} wrong number of input arguments for {3}.{4} (called with {5} args, want {6})")
	errBadInputArg       = verror.Register("v.io/x/ref/profiles/internal/ipc.badInputArg", verror.NoRetry, "{1:}{2:} failed to decode request {3}.{4} arg #{5}: {6}")
	errBadBlessings      = verror.Register("v.io/x/ref/profiles/internal/ipc.badBlessings", verror.NoRetry, "{1:}{2:} failed to decode blessings: {3}")
	errBadBlessingsCache = verror.Register("v.io/x/ref/profiles/internal/ipc.badBlessingsCache", verror.NoRetry, "{1:}{2:} failed to find blessings in cache: {3}")
	errBadDischarge      = verror.Register("v.io/x/ref/profiles/internal/ipc.badDischarge", verror.NoRetry, "{1:}{2:} failed to decode discharge #{3}: {4}")
	errBadAuth           = verror.Register("v.io/x/ref/profiles/internal/ipc.badAuth", verror.NoRetry, "{1:}{2:} not authorized to call {3}.{4}: {5}")
)

func init() {
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(ErrInvalidBlessings.ID), "{1:}{2:} All valid blessings for this request: {3} (rejected {4}) are disallowed by the policy {5} (rejected {6})")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadRequest.ID), "{1:}{2:} failed to decode request: {3}")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadNumInputArgs.ID), "{1:}{2:} wrong number of input arguments for {3}.{4} (called with {5} args, want {6})")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadInputArg.ID), "{1:}{2:} failed to decode request {3}.{4} arg #{5}: {6}")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadBlessings.ID), "{1:}{2:} failed to decode blessings: {3}")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadBlessingsCache.ID), "{1:}{2:} failed to find blessings in cache: {3}")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadDischarge.ID), "{1:}{2:} failed to decode discharge #{3}: {4}")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(errBadAuth.ID), "{1:}{2:} not authorized to call {3}.{4}: {5}")
}

// NewErrInvalidBlessings returns an error with the ErrInvalidBlessings ID.
func NewErrInvalidBlessings(ctx *context.T, remote []string, remoteErr []security.RejectedBlessing, local []string, localErr []security.RejectedBlessing) error {
	return verror.New(ErrInvalidBlessings, ctx, remote, remoteErr, local, localErr)
}

// newErrBadRequest returns an error with the errBadRequest ID.
func newErrBadRequest(ctx *context.T, err error) error {
	return verror.New(errBadRequest, ctx, err)
}

// newErrBadNumInputArgs returns an error with the errBadNumInputArgs ID.
func newErrBadNumInputArgs(ctx *context.T, suffix string, method string, numCalled uint64, numWanted uint64) error {
	return verror.New(errBadNumInputArgs, ctx, suffix, method, numCalled, numWanted)
}

// newErrBadInputArg returns an error with the errBadInputArg ID.
func newErrBadInputArg(ctx *context.T, suffix string, method string, index uint64, err error) error {
	return verror.New(errBadInputArg, ctx, suffix, method, index, err)
}

// newErrBadBlessings returns an error with the errBadBlessings ID.
func newErrBadBlessings(ctx *context.T, err error) error {
	return verror.New(errBadBlessings, ctx, err)
}

// newErrBadBlessingsCache returns an error with the errBadBlessingsCache ID.
func newErrBadBlessingsCache(ctx *context.T, err error) error {
	return verror.New(errBadBlessingsCache, ctx, err)
}

// newErrBadDischarge returns an error with the errBadDischarge ID.
func newErrBadDischarge(ctx *context.T, index uint64, err error) error {
	return verror.New(errBadDischarge, ctx, index, err)
}

// newErrBadAuth returns an error with the errBadAuth ID.
func newErrBadAuth(ctx *context.T, suffix string, method string, err error) error {
	return verror.New(errBadAuth, ctx, suffix, method, err)
}
