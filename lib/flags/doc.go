// Package flags provides definitions for commonly used flags and, where
// appropriate, implementations of the flag.Value interface for those flags to
// ensure that only valid values of those flags are supplied. Some of these
// flags may also be specified using environment variables directly and are
// documented accordingly; in these cases the command line value takes
// precedence over the environment variable.
//
// Flags are defined as 'groups' of related flags so that the caller may choose
// which ones to use without having to be burdened with the full set. The groups
// may be used directly or via the Flags type that aggregates multiple
// groups. In all cases, the flags are registered with a supplied flag.FlagSet
// and hence are not forced onto the command line unless the caller passes in
// flag.CommandLine as the flag.FlagSet to use.
//
// In general, this package will be used by vanadium profiles and the runtime
// implementations, but can also be used by any application that wants access to
// the flags and environment variables it supports.
package flags
