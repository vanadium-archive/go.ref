// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package profiles and its subdirectories provide implementations of the
// Vanadium runtime for different runtime environments.  Each subdirectory is a
// package that implements the v23.Profile function.
//
// Profiles register themselves by calling v.io/v23/rt.RegisterProfile in an
// init function.  Users choose a particular profile implementation by importing
// the appropriate package in their main package.  It is an error to import more
// than one profile, and the registration mechanism will panic if this is
// attempted.  Commonly used functionality and pre-canned profiles are in
// profiles/internal.
//
// This top level directory contains a 'generic' Profile and utility routines
// used by other Profiles. It should be imported whenever possible and
// particularly by tests.
//
// The 'roaming' Profile adds operating system support for varied network
// configurations and in particular dhcp. It should be used by any application
// that may 'roam' or any may be behind a 1-1 NAT. The 'static' profile
// does not provide dhcp support, but is otherwise like the roaming profile.
package profiles
